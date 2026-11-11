// Package server 提供 REST API 与内嵌前端页面。
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ticketsystem/internal/store"
)

// Server HTTP 服务
type Server struct {
	store   *store.Store
	assets  fs.FS
	version string // 展示在界面角落的版本号
}

// New 构造服务，assets 为前端静态文件根目录
func New(st *store.Store, assets fs.FS, version string) *Server {
	return &Server{store: st, assets: assets, version: version}
}

// Handler 返回路由
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/tickets", s.listTickets)
	mux.HandleFunc("POST /api/tickets", s.createTicket)
	mux.HandleFunc("GET /api/tickets/{id}", s.getTicket)
	mux.HandleFunc("PUT /api/tickets/{id}", s.updateTicket)
	mux.HandleFunc("PATCH /api/tickets/{id}", s.patchTicket)
	mux.HandleFunc("DELETE /api/tickets", s.deleteTickets)
	mux.HandleFunc("GET /api/next-id", s.nextID)
	mux.HandleFunc("GET /api/stats", s.stats)
	mux.HandleFunc("GET /api/export", s.exportTickets)
	mux.HandleFunc("GET /api/version", s.getVersion)

	mux.Handle("/", http.FileServer(http.FS(s.assets)))

	return logMiddleware(mux)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %d %dB %s", r.Method, r.URL.RequestURI(),
				sw.status, sw.written, time.Since(start).Round(time.Millisecond))
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status  int
	written int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.written += n
	return n, err
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("写响应失败: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decode 限制请求体大小，避免超大 payload
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "请求格式错误: "+err.Error())
		return false
	}
	return true
}

// ---------- 列表 ----------

func (s *Server) listTickets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	atoi := func(k string) int {
		n, _ := strconv.Atoi(q.Get(k))
		return n
	}
	f := store.Filter{
		Keyword:   q.Get("q"),
		Priority:  atoi("priority"),
		Status:    atoi("status"),
		HideClose: q.Get("hideClosed") == "1",
	}
	list, total, err := s.store.List(f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":   total,
		"showing": len(list),
		"items":   list,
	})
}

// ---------- 详情 ----------

func (s *Server) getTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ---------- 新建 ----------

type ticketPayload struct {
	TicketID      string `json:"ticketId"`
	ClientName    string `json:"clientName"`
	ContactPerson string `json:"contactPerson"`
	ContactPhone  string `json:"contactPhone"`
	Description   string `json:"description"`
	Record        string `json:"record"`
	Rise          bool   `json:"rise"`
	Closed        bool   `json:"closed"`
	Priority      int    `json:"priority"`
	CreatedDate   string `json:"createdDate"` // YYYY-MM-DD，可空=今天
}

func (p *ticketPayload) validate() string {
	p.TicketID = strings.TrimSpace(p.TicketID)
	p.ClientName = strings.TrimSpace(p.ClientName)
	p.Description = strings.TrimSpace(p.Description)

	switch {
	case p.TicketID == "":
		return "单号不能为空"
	case len(p.TicketID) > 64:
		return "单号过长（最多 64 字符）"
	case p.ClientName == "":
		return "客户名称不能为空"
	case len(p.ClientName) > 200:
		return "客户名称过长（最多 200 字符）"
	case p.Description == "":
		return "问题描述不能为空"
	case p.Priority < 1 || p.Priority > len(store.TypeNames):
		return "项目类型无效"
	case len(p.ContactPerson) > 100:
		return "联系人过长（最多 100 字符）"
	case len(p.ContactPhone) > 64:
		return "联系方式过长（最多 64 字符）"
	}
	return ""
}

func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	var p ticketPayload
	if !decode(w, r, &p) {
		return
	}
	if msg := p.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}

	t := &store.Ticket{
		TicketID:      p.TicketID,
		ClientName:    p.ClientName,
		ContactPerson: p.ContactPerson,
		ContactPhone:  p.ContactPhone,
		Description:   p.Description,
		Record:        p.Record,
		Rise:          p.Rise,
		Closed:        p.Closed,
		Priority:      p.Priority,
	}
	if p.CreatedDate != "" {
		d, err := time.ParseInLocation("2006-01-02", p.CreatedDate, time.Local)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "创建日期格式应为 YYYY-MM-DD")
			return
		}
		t.CreatedAt = d
	}
	if err := s.store.Create(t); err != nil {
		respondStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// ---------- 更新 ----------

func (s *Server) updateTicket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p ticketPayload
	if !decode(w, r, &p) {
		return
	}
	p.TicketID = id
	if msg := p.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	t := &store.Ticket{
		TicketID:      id,
		ClientName:    p.ClientName,
		ContactPerson: p.ContactPerson,
		ContactPhone:  p.ContactPhone,
		Description:   p.Description,
		Record:        p.Record,
		Rise:          p.Rise,
		Closed:        p.Closed,
		Priority:      p.Priority,
	}
	if err := s.store.Update(t); err != nil {
		respondStoreErr(w, err)
		return
	}
	cur, err := s.store.Get(id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cur)
}

// patchPayload 局部更新体，指针区分「未提供」与「零值」
type patchPayload struct {
	Record        *string `json:"record"`
	Rise          *bool   `json:"rise"`
	Closed        *bool   `json:"closed"`
	Description   *string `json:"description"`
	ContactPerson *string `json:"contactPerson"`
	ContactPhone  *string `json:"contactPhone"`
	ClientName    *string `json:"clientName"`
	Priority      *int    `json:"priority"`
}

func (s *Server) patchTicket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p patchPayload
	if !decode(w, r, &p) {
		return
	}

	type kv struct {
		field string
		value any
	}
	var updates []kv
	if p.Record != nil {
		updates = append(updates, kv{"record", *p.Record})
	}
	if p.Rise != nil {
		updates = append(updates, kv{"rise", *p.Rise})
	}
	if p.Closed != nil {
		updates = append(updates, kv{"closed", *p.Closed})
	}
	if p.Description != nil {
		v := strings.TrimSpace(*p.Description)
		if v == "" {
			writeErr(w, http.StatusBadRequest, "问题描述不能为空")
			return
		}
		updates = append(updates, kv{"description", v})
	}
	if p.ContactPerson != nil {
		updates = append(updates, kv{"contactPerson", *p.ContactPerson})
	}
	if p.ContactPhone != nil {
		updates = append(updates, kv{"contactPhone", *p.ContactPhone})
	}
	if p.ClientName != nil {
		v := strings.TrimSpace(*p.ClientName)
		if v == "" {
			writeErr(w, http.StatusBadRequest, "客户名称不能为空")
			return
		}
		updates = append(updates, kv{"clientName", v})
	}
	if p.Priority != nil {
		if *p.Priority < 1 || *p.Priority > len(store.TypeNames) {
			writeErr(w, http.StatusBadRequest, "项目类型无效")
			return
		}
		updates = append(updates, kv{"priority", *p.Priority})
	}
	if len(updates) == 0 {
		writeErr(w, http.StatusBadRequest, "没有需要更新的字段")
		return
	}

	for _, u := range updates {
		if err := s.store.Patch(id, u.field, u.value); err != nil {
			respondStoreErr(w, err)
			return
		}
	}
	cur, err := s.store.Get(id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cur)
}

// ---------- 删除 ----------

func (s *Server) deleteTickets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "请先选择要删除的工单")
		return
	}
	n, err := s.store.Delete(body.IDs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("删除工单 %d 条", n)
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

// ---------- 单号 ----------

func (s *Server) nextID(w http.ResponseWriter, r *http.Request) {
	p, _ := strconv.Atoi(r.URL.Query().Get("priority"))
	id, err := s.store.NextTicketID(p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ticketId": id})
}

// ---------- 统计 ----------

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ---------- 版本 ----------

func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

// ---------- 导出 ----------

func (s *Server) exportTickets(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	clean := ids[:0]
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		writeErr(w, http.StatusBadRequest, "未指定要导出的工单")
		return
	}

	tickets, err := s.store.GetMany(clean)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(tickets) == 0 {
		writeErr(w, http.StatusNotFound, "未找到工单")
		return
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	ts := time.Now().Format("20060102_150405")

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=\"tickets_%s.csv\"; filename*=UTF-8''%s", ts, urlEnc("工单导出_"+ts+".csv")))
		// BOM 让 Excel 正确识别 UTF-8
		w.Write([]byte{0xEF, 0xBB, 0xBF})
		if err := writeCSV(w, tickets); err != nil {
			log.Printf("导出 CSV 失败: %v", err)
		}
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=\"tickets_%s.txt\"; filename*=UTF-8''%s", ts, urlEnc("工单导出_"+ts+".txt")))
		if err := writeTXT(w, tickets); err != nil {
			log.Printf("导出 TXT 失败: %v", err)
		}
	}
	log.Printf("导出 %d 条工单 (%s)", len(tickets), format)
}

func respondStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "工单不存在")
	case errors.Is(err, store.ErrDuplicate):
		writeErr(w, http.StatusConflict, "该单号已存在")
	default:
		log.Printf("数据操作失败: %v", err)
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// FreePort 获取一个空闲端口
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
