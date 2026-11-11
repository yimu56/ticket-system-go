// Package store 提供工单数据的持久化访问。
// 表结构与原 Python 版本 (PyQt5 + SQLAlchemy) 完全兼容，可直接读取存量 ticket.db。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无需 CGO
)

// 项目类型（原 priority 字段）
const (
	TypeDelivery = 1 // 交付
	TypeTest     = 2 // 测试
	TypeAfter    = 3 // 售后
)

// 工单状态（由字段推导，不落库）
const (
	StatusNew     = "新工单"
	StatusDoing   = "处理中"
	StatusTimeout = "处理中(超时)"
	StatusRise    = "已上升"
	StatusClosed  = "已闭环"
)

// TimeoutDays 超过该天数仍未闭环视为超时
const TimeoutDays = 7

// TypeNames 下标即 priority 值
var TypeNames = []string{"交付", "测试", "售后"}

// TypePrefix 自动单号前缀；售后不自动生成（沿用外部系统编号）
var TypePrefix = map[int]string{TypeDelivery: "JF", TypeTest: "CS"}

// ErrNotFound 工单不存在
var ErrNotFound = errors.New("工单不存在")

// ErrDuplicate 单号已存在
var ErrDuplicate = errors.New("单号已存在")

// Ticket 工单
type Ticket struct {
	ID            int64     `json:"id"`
	TicketID      string    `json:"ticketId"`
	ClientName    string    `json:"clientName"`
	ContactPerson string    `json:"contactPerson"`
	ContactPhone  string    `json:"contactPhone"`
	Description   string    `json:"description"`
	Record        string    `json:"record"`
	Rise          bool      `json:"rise"`
	Closed        bool      `json:"closed"`
	Priority      int       `json:"priority"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Status        string    `json:"status"`
}

// TypeName 项目类型显示名
func (t *Ticket) TypeName() string {
	if t.Priority >= 1 && t.Priority <= len(TypeNames) {
		return TypeNames[t.Priority-1]
	}
	return "未知"
}

// StatusOf 推导状态：闭环 > 上升 > 新建 > 超时 > 处理中
func StatusOf(closed, rise bool, created time.Time, now time.Time) string {
	switch {
	case closed:
		return StatusClosed
	case rise:
		return StatusRise
	}
	d := daysBetween(created, now)
	if d == 0 {
		return StatusNew
	}
	if d > TimeoutDays {
		return StatusTimeout
	}
	return StatusDoing
}

func daysBetween(from, now time.Time) int {
	y1, m1, d1 := from.Date()
	y2, m2, d2 := now.Date()
	a := time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)
	b := time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// Filter 查询条件。所有过滤都下推到 SQL。
type Filter struct {
	Keyword   string // 单号/客户/联系人/电话/描述 模糊匹配
	Priority  int    // 0 = 全部
	Status    int    // 0全部 1未闭环 2已闭环 3已上升
	HideClose bool   // 隐藏已闭环
}

// Store 数据访问层
type Store struct {
	db *sql.DB
}

// Open 打开（或新建）数据库并确保表结构存在
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// 本地单机工具，串行访问足够，避免连接竞争
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ensureSchema() error {
	const ddl = `CREATE TABLE IF NOT EXISTS tickets (
		id INTEGER NOT NULL,
		ticket_id VARCHAR NOT NULL,
		client_name VARCHAR NOT NULL,
		contact_person VARCHAR,
		contact_phone VARCHAR,
		description VARCHAR,
		record VARCHAR,
		rise BOOLEAN,
		closed BOOLEAN,
		priority INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		PRIMARY KEY (id),
		UNIQUE (ticket_id)
	)`
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("创建表失败: %w", err)
	}
	// 索引：单号唯一索引已由 UNIQUE 约束覆盖，这里补状态/类型筛选与排序用索引
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_tickets_closed ON tickets(closed)`,
		`CREATE INDEX IF NOT EXISTS idx_tickets_priority ON tickets(priority)`,
		`CREATE INDEX IF NOT EXISTS idx_tickets_created ON tickets(created_at DESC)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("创建索引失败: %w", err)
		}
	}
	return nil
}

// buildWhere 根据 Filter 生成 WHERE 子句与参数
func buildWhere(f Filter) (string, []any) {
	var conds []string
	var args []any

	if f.HideClose {
		conds = append(conds, "closed = 0")
	}
	if f.Priority > 0 {
		conds = append(conds, "priority = ?")
		args = append(args, f.Priority)
	}
	switch f.Status {
	case 1:
		conds = append(conds, "closed = 0")
	case 2:
		conds = append(conds, "closed = 1")
	case 3:
		conds = append(conds, "rise = 1")
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		conds = append(conds,
			`(ticket_id LIKE ? ESCAPE '\' OR client_name LIKE ? ESCAPE '\' OR
			  contact_person LIKE ? ESCAPE '\' OR contact_phone LIKE ? ESCAPE '\' OR
			  description LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// escapeLike 转义 LIKE 通配符，防止用户输入 % _ 造成全表扫描
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

const columns = `id, ticket_id, client_name, contact_person, contact_phone,
	description, record, rise, closed, priority, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanTicket(sc scanner) (*Ticket, error) {
	var t Ticket
	var cp, cphone, desc, rec sql.NullString
	var rise, closed sql.NullBool
	var prio sql.NullInt64
	var created, updated sql.NullTime

	if err := sc.Scan(&t.ID, &t.TicketID, &t.ClientName, &cp, &cphone,
		&desc, &rec, &rise, &closed, &prio, &created, &updated); err != nil {
		return nil, err
	}
	t.ContactPerson = cp.String
	t.ContactPhone = cphone.String
	t.Description = desc.String
	t.Record = rec.String
	t.Rise = rise.Bool
	t.Closed = closed.Bool
	t.Priority = int(prio.Int64)
	if t.Priority < 1 || t.Priority > len(TypeNames) {
		t.Priority = TypeAfter
	}
	t.CreatedAt = localize(created.Time)
	if updated.Valid {
		t.UpdatedAt = localize(updated.Time)
	} else {
		t.UpdatedAt = t.CreatedAt
	}
	t.Status = StatusOf(t.Closed, t.Rise, t.CreatedAt, time.Now())
	return &t, nil
}

// localize SQLite 里的 DATETIME 是不带时区的本地时间，
// 驱动会按 UTC 解析；这里统一改回本地时区，避免前端再叠加一次时差。
func localize(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local)
}

// List 按条件查询，返回结果与总数
func (s *Store) List(f Filter) ([]*Ticket, int, error) {
	where, args := buildWhere(f)

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM tickets"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计失败: %w", err)
	}

	// 排序：未闭环在前 → 交付/测试/售后 → 最新在前
	q := "SELECT " + columns + " FROM tickets" + where +
		" ORDER BY closed ASC, priority ASC, created_at DESC, id DESC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询失败: %w", err)
	}
	defer rows.Close()

	list := make([]*Ticket, 0, total)
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("读取记录失败: %w", err)
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历结果失败: %w", err)
	}
	return list, total, nil
}

// Get 按单号取单条
func (s *Store) Get(ticketID string) (*Ticket, error) {
	row := s.db.QueryRow("SELECT "+columns+" FROM tickets WHERE ticket_id = ?", ticketID)
	t, err := scanTicket(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询工单失败: %w", err)
	}
	return t, nil
}

// NextTicketID 生成当天下一个单号；售后返回空串表示需手工填写
func (s *Store) NextTicketID(priority int) (string, error) {
	prefix, ok := TypePrefix[priority]
	if !ok {
		return "", nil
	}
	today := time.Now().Format("20060102")
	pattern := prefix + "-" + today + "-%"

	rows, err := s.db.Query(
		"SELECT ticket_id FROM tickets WHERE ticket_id LIKE ? ESCAPE '\\'", pattern)
	if err != nil {
		return "", fmt.Errorf("查询单号失败: %w", err)
	}
	defer rows.Close()

	maxSeq := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		parts := strings.Split(id, "-")
		if len(parts) < 3 {
			continue
		}
		seq := 0
		if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &seq); err != nil {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%02d", prefix, today, maxSeq+1), nil
}

// Stats 概览统计
type Stats struct {
	Total     int `json:"total"`
	Open      int `json:"open"`
	Closed    int `json:"closed"`
	Rise      int `json:"rise"`
	Timeout   int `json:"timeout"`
	Today     int `json:"today"`
	Delivery  int `json:"delivery"`
	Test      int `json:"test"`
	AfterSale int `json:"afterSale"`
}

// Stats 汇总统计（全部下推到 SQL，只取回 1 行）
func (s *Store) Stats() (*Stats, error) {
	st := &Stats{}
	now := time.Now()
	// 时间边界在 Go 侧算好再传参，避免依赖 SQLite 的 localtime 与进程时区是否一致
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	timeoutBefore := todayStart.AddDate(0, 0, -TimeoutDays)

	q := `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN closed = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN closed = 1 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN rise = 1 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN closed = 0 AND rise = 0
			AND created_at < ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN priority = 1 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN priority = 2 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN priority = 3 THEN 1 ELSE 0 END), 0)
		FROM tickets`
	err := s.db.QueryRow(q, timeoutBefore, todayStart).Scan(
		&st.Total, &st.Open, &st.Closed, &st.Rise, &st.Timeout, &st.Today,
		&st.Delivery, &st.Test, &st.AfterSale)
	if err != nil {
		return nil, fmt.Errorf("统计失败: %w", err)
	}
	return st, nil
}

// Create 新建工单
func (s *Store) Create(t *Ticket) error {
	if t.Priority < 1 || t.Priority > len(TypeNames) {
		t.Priority = TypeAfter
	}
	now := time.Now()
	if t.CreatedAt.IsZero() {
		// 保留“选择日期 + 当前时刻”的原有语义
		t.CreatedAt = now
	} else {
		h, m, sec := now.Clock()
		t.CreatedAt = time.Date(t.CreatedAt.Year(), t.CreatedAt.Month(), t.CreatedAt.Day(),
			h, m, sec, 0, t.CreatedAt.Location())
	}
	t.UpdatedAt = now

	res, err := s.db.Exec(`INSERT INTO tickets
		(ticket_id, client_name, contact_person, contact_phone, description, record,
		 rise, closed, priority, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		t.TicketID, t.ClientName, t.ContactPerson, t.ContactPhone, t.Description, t.Record,
		t.Rise, t.Closed, t.Priority, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		if isUniqueErr(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("保存工单失败: %w", err)
	}
	t.ID, _ = res.LastInsertId()
	t.Status = StatusOf(t.Closed, t.Rise, t.CreatedAt, time.Now())
	return nil
}

// Update 更新工单全部可编辑字段（不含单号与创建时间）
func (s *Store) Update(t *Ticket) error {
	t.UpdatedAt = time.Now()
	if t.Priority < 1 || t.Priority > len(TypeNames) {
		t.Priority = TypeAfter
	}
	res, err := s.db.Exec(`UPDATE tickets SET
		client_name = ?, contact_person = ?, contact_phone = ?, description = ?, record = ?,
		rise = ?, closed = ?, priority = ?, updated_at = ?
		WHERE ticket_id = ?`,
		t.ClientName, t.ContactPerson, t.ContactPhone, t.Description, t.Record,
		t.Rise, t.Closed, t.Priority, t.UpdatedAt, t.TicketID)
	if err != nil {
		return fmt.Errorf("更新工单失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Patch 局部更新。field 支持 record/rise/closed/description/contactPerson/contactPhone/clientName/priority
func (s *Store) Patch(ticketID, field string, value any) error {
	col, ok := patchableColumns[field]
	if !ok {
		return fmt.Errorf("不支持更新的字段: %s", field)
	}
	q := "UPDATE tickets SET " + col + " = ?, updated_at = ? WHERE ticket_id = ?"
	res, err := s.db.Exec(q, value, time.Now(), ticketID)
	if err != nil {
		return fmt.Errorf("更新失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

var patchableColumns = map[string]string{
	"record":        "record",
	"rise":          "rise",
	"closed":        "closed",
	"description":   "description",
	"contactPerson": "contact_person",
	"contactPhone":  "contact_phone",
	"clientName":    "client_name",
	"priority":      "priority",
}

// Delete 批量删除，返回实际删除条数（单事务）
func (s *Store) Delete(ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("DELETE FROM tickets WHERE ticket_id = ?")
	if err != nil {
		return 0, fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	n := 0
	for _, id := range ids {
		res, err := stmt.Exec(id)
		if err != nil {
			return 0, fmt.Errorf("删除 %s 失败: %w", id, err)
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n += int(c)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return n, nil
}

// GetMany 按单号批量取（导出用），保持传入顺序
func (s *Store) GetMany(ids []string) ([]*Ticket, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := "SELECT " + columns + " FROM tickets WHERE ticket_id IN (" +
		strings.Join(placeholders, ",") + ") ORDER BY priority ASC, created_at DESC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}
	defer rows.Close()

	out := make([]*Ticket, 0, len(ids))
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, fmt.Errorf("读取记录失败: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}
