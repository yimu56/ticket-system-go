package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"ticketsystem/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	assets := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>t</title>")},
	}
	ts := httptest.NewServer(New(st, assets, "v1.0.0").Handler())
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, ts *httptest.Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

func createTicket(t *testing.T, ts *httptest.Server, id, client string) {
	t.Helper()
	code, body := do(t, ts, "POST", "/api/tickets", map[string]any{
		"ticketId": id, "clientName": client, "description": "描述", "priority": 1,
	})
	if code != http.StatusCreated {
		t.Fatalf("创建失败 %d: %s", code, body)
	}
}

func TestCreateAndGet(t *testing.T) {
	ts := newTestServer(t)
	createTicket(t, ts, "JF-1", "客户A")

	code, body := do(t, ts, "GET", "/api/tickets/JF-1", nil)
	if code != http.StatusOK {
		t.Fatalf("查询失败 %d: %s", code, body)
	}
	var got store.Ticket
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.ClientName != "客户A" {
		t.Fatalf("客户名不一致: %+v", got)
	}
}

func TestValidation(t *testing.T) {
	ts := newTestServer(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"空单号", map[string]any{"clientName": "a", "description": "d", "priority": 1}},
		{"空客户名", map[string]any{"ticketId": "x", "clientName": "  ", "description": "d", "priority": 1}},
		{"空描述", map[string]any{"ticketId": "x", "clientName": "a", "description": "", "priority": 1}},
		{"类型越界", map[string]any{"ticketId": "x", "clientName": "a", "description": "d", "priority": 9}},
	}
	for _, c := range cases {
		if code, body := do(t, ts, "POST", "/api/tickets", c.body); code != http.StatusBadRequest {
			t.Errorf("%s 应返回 400，实际 %d: %s", c.name, code, body)
		}
	}
}

func TestDuplicateAndNotFound(t *testing.T) {
	ts := newTestServer(t)
	createTicket(t, ts, "D-1", "客户")

	code, _ := do(t, ts, "POST", "/api/tickets", map[string]any{
		"ticketId": "D-1", "clientName": "客户2", "description": "d", "priority": 1,
	})
	if code != http.StatusConflict {
		t.Fatalf("重复单号应 409，实际 %d", code)
	}
	if code, _ := do(t, ts, "GET", "/api/tickets/NONE", nil); code != http.StatusNotFound {
		t.Fatalf("不存在应 404，实际 %d", code)
	}
}

func TestPatchAndList(t *testing.T) {
	ts := newTestServer(t)
	createTicket(t, ts, "P-1", "甲")
	createTicket(t, ts, "P-2", "乙")

	code, body := do(t, ts, "PATCH", "/api/tickets/P-1", map[string]any{"closed": true})
	if code != http.StatusOK {
		t.Fatalf("PATCH 失败 %d: %s", code, body)
	}
	var got store.Ticket
	json.Unmarshal(body, &got)
	if !got.Closed || got.Status != store.StatusClosed {
		t.Fatalf("闭环未生效: %+v", got)
	}

	// hideClosed=1 应只剩 1 条
	code, body = do(t, ts, "GET", "/api/tickets?hideClosed=1", nil)
	if code != http.StatusOK {
		t.Fatalf("列表失败 %d", code)
	}
	var list struct {
		Total   int             `json:"total"`
		Showing int             `json:"showing"`
		Items   []*store.Ticket `json:"items"`
	}
	json.Unmarshal(body, &list)
	if list.Total != 1 || list.Showing != 1 {
		t.Fatalf("筛选结果错误: %+v", list)
	}
}

func TestDeleteAndExport(t *testing.T) {
	ts := newTestServer(t)
	createTicket(t, ts, "E-1", "客户一")
	createTicket(t, ts, "E-2", "客户二")

	code, body := do(t, ts, "GET", "/api/export?format=csv&ids=E-1,E-2", nil)
	if code != http.StatusOK {
		t.Fatalf("导出失败 %d: %s", code, body)
	}
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("CSV 缺少 UTF-8 BOM")
	}
	if !strings.Contains(string(body), "客户一") {
		t.Fatal("CSV 内容缺失")
	}

	code, body = do(t, ts, "DELETE", "/api/tickets", map[string]any{"ids": []string{"E-1", "E-2"}})
	if code != http.StatusOK {
		t.Fatalf("删除失败 %d: %s", code, body)
	}
	if !strings.Contains(string(body), `"deleted":2`) {
		t.Fatalf("删除条数错误: %s", body)
	}
	if code, _ := do(t, ts, "GET", "/api/tickets/E-1", nil); code != http.StatusNotFound {
		t.Fatalf("删除后应 404，实际 %d", code)
	}
}

func TestNextID(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, ts, "GET", "/api/next-id?priority=3", nil)
	if code != http.StatusOK {
		t.Fatalf("请求失败 %d", code)
	}
	var r struct {
		TicketID string `json:"ticketId"`
	}
	json.Unmarshal(body, &r)
	if r.TicketID != "" {
		t.Fatalf("售后不应自动生成，实际 %q", r.TicketID)
	}
}

func TestStaticAsset(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, ts, "GET", "/", nil)
	if code != http.StatusOK || !strings.Contains(string(body), "<!doctype html>") {
		t.Fatalf("首页异常 %d: %s", code, body)
	}
}

func TestVersion(t *testing.T) {
	ts := newTestServer(t)
	code, body := do(t, ts, "GET", "/api/version", nil)
	if code != http.StatusOK {
		t.Fatalf("版本接口失败 %d", code)
	}
	var r struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	if r.Version != "v1.0.0" {
		t.Fatalf("版本号错误: %q", r.Version)
	}
}
