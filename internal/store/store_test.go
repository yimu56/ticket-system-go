package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCRUD(t *testing.T) {
	s := newTestStore(t)

	in := &Ticket{
		TicketID: "JF-20260101-01", ClientName: "测试客户",
		Description: "描述", Priority: TypeDelivery,
	}
	if err := s.Create(in); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if in.ID == 0 {
		t.Fatal("创建后未回填 ID")
	}

	got, err := s.Get(in.TicketID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.ClientName != in.ClientName || got.Priority != TypeDelivery {
		t.Fatalf("字段不一致: %+v", got)
	}

	if err := s.Patch(in.TicketID, "closed", true); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	got, _ = s.Get(in.TicketID)
	if !got.Closed || got.Status != StatusClosed {
		t.Fatalf("闭环未生效: %+v", got)
	}

	n, err := s.Delete([]string{in.TicketID})
	if err != nil || n != 1 {
		t.Fatalf("删除失败 n=%d err=%v", n, err)
	}
	if _, err := s.Get(in.TicketID); err != ErrNotFound {
		t.Fatalf("删除后仍可查到: %v", err)
	}
}

func TestDuplicate(t *testing.T) {
	s := newTestStore(t)
	tk := &Ticket{TicketID: "D-1", ClientName: "a", Description: "d", Priority: TypeDelivery}
	if err := s.Create(tk); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	err := s.Create(&Ticket{TicketID: "D-1", ClientName: "b", Description: "d"})
	if err != ErrDuplicate {
		t.Fatalf("重复单号应返回 ErrDuplicate，实际: %v", err)
	}
}

func TestNextTicketID(t *testing.T) {
	s := newTestStore(t)
	today := time.Now().Format("20060102")

	// 首条应为 01
	id, err := s.NextTicketID(TypeDelivery)
	if err != nil {
		t.Fatal(err)
	}
	if want := "JF-" + today + "-01"; id != want {
		t.Fatalf("期望 %s，实际 %s", want, id)
	}

	// 序号应递增
	for _, want := range []string{"01", "02", "03"} {
		s.Create(&Ticket{TicketID: "JF-" + today + "-" + want, ClientName: "x", Description: "d"})
	}
	id, _ = s.NextTicketID(TypeDelivery)
	if want := "JF-" + today + "-04"; id != want {
		t.Fatalf("期望 %s，实际 %s", want, id)
	}

	// 售后不自动生成
	if id, _ := s.NextTicketID(TypeAfter); id != "" {
		t.Fatalf("售后应返回空串，实际 %s", id)
	}
}

func TestFilter(t *testing.T) {
	s := newTestStore(t)
	rows := []*Ticket{
		{TicketID: "A-1", ClientName: "甲方公司", Description: "网络故障", Priority: TypeDelivery},
		{TicketID: "A-2", ClientName: "乙方公司", Description: "设备上架", Priority: TypeTest, Closed: true},
		{TicketID: "A-3", ClientName: "丙方公司", Description: "网络割接", Priority: TypeAfter, Rise: true},
	}
	for _, r := range rows {
		if err := s.Create(r); err != nil {
			t.Fatal(err)
		}
	}

	if l, _, _ := s.List(Filter{}); len(l) != 3 {
		t.Fatalf("全部应 3 条，实际 %d", len(l))
	}
	if l, _, _ := s.List(Filter{HideClose: true}); len(l) != 2 {
		t.Fatalf("隐藏闭环应 2 条，实际 %d", len(l))
	}
	if l, _, _ := s.List(Filter{Status: 2}); len(l) != 1 || l[0].TicketID != "A-2" {
		t.Fatalf("已闭环筛选错误: %d", len(l))
	}
	if l, _, _ := s.List(Filter{Status: 3}); len(l) != 1 || l[0].TicketID != "A-3" {
		t.Fatalf("已上升筛选错误: %d", len(l))
	}
	if l, _, _ := s.List(Filter{Priority: TypeTest}); len(l) != 1 {
		t.Fatalf("类型筛选错误: %d", len(l))
	}
	// 关键词应命中描述
	if l, _, _ := s.List(Filter{Keyword: "网络"}); len(l) != 2 {
		t.Fatalf("关键词应 2 条，实际 %d", len(l))
	}
	// LIKE 通配符必须被转义，% 不应匹配全部
	if l, _, _ := s.List(Filter{Keyword: "%"}); len(l) != 0 {
		t.Fatalf("通配符未被转义，命中 %d 条", len(l))
	}
}

func TestStatusOf(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)
	cases := []struct {
		name    string
		closed  bool
		rise    bool
		created time.Time
		want    string
	}{
		{"闭环优先", true, true, now, StatusClosed},
		{"上升", false, true, now, StatusRise},
		{"今天", false, false, now, StatusNew},
		{"3 天内", false, false, now.AddDate(0, 0, -3), StatusDoing},
		{"刚好 7 天未超时", false, false, now.AddDate(0, 0, -7), StatusDoing},
		{"超过 7 天", false, false, now.AddDate(0, 0, -8), StatusTimeout},
	}
	for _, c := range cases {
		if got := StatusOf(c.closed, c.rise, c.created, now); got != c.want {
			t.Errorf("%s: 期望 %s，实际 %s", c.name, c.want, got)
		}
	}
}

func TestStats(t *testing.T) {
	s := newTestStore(t)
	s.Create(&Ticket{TicketID: "S-1", ClientName: "a", Description: "d", Priority: TypeDelivery})
	s.Create(&Ticket{TicketID: "S-2", ClientName: "b", Description: "d", Priority: TypeAfter, Closed: true})
	s.Create(&Ticket{TicketID: "S-3", ClientName: "c", Description: "d", Priority: TypeTest, Rise: true})

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 3 || st.Open != 2 || st.Closed != 1 || st.Rise != 1 {
		t.Fatalf("统计错误: %+v", st)
	}
	if st.Delivery != 1 || st.Test != 1 || st.AfterSale != 1 {
		t.Fatalf("类型统计错误: %+v", st)
	}
	if st.Today != 3 {
		t.Fatalf("今日新增应为 3，实际 %d", st.Today)
	}
}

func TestOpenLegacyDB(t *testing.T) {
	// 模拟旧版 Python 建的库（bool 以 0/1 存储，时间带微秒）
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	defer s.Close()

	seed := `INSERT INTO tickets VALUES
		(1,'JF-20250819-01','星德胜','张三','139','问题','记录',0,0,1,
		 '2025-08-19 17:23:11.259496','2025-09-15 09:00:09.967438');`
	if _, err := s.db.Exec(seed); err != nil {
		t.Fatalf("写入旧数据失败: %v", err)
	}

	got, err := s.Get("JF-20250819-01")
	if err != nil {
		t.Fatalf("读取旧数据失败: %v", err)
	}
	if got.ClientName != "星德胜" || got.ContactPerson != "张三" || got.Rise || got.Closed {
		t.Fatalf("旧数据解析错误: %+v", got)
	}
	if got.CreatedAt.Format("2006-01-02") != "2025-08-19" {
		t.Fatalf("时间解析错误: %v", got.CreatedAt)
	}
	// 时间必须是本地时区，且精度保留到微秒（不能再被当成 UTC 平移）
	if got.CreatedAt.Hour() != 17 || got.CreatedAt.Minute() != 23 {
		t.Fatalf("时分被时区改写了: %v", got.CreatedAt)
	}
	if got.CreatedAt.Location() != time.Local {
		t.Fatalf("时区应为本地，实际 %v", got.CreatedAt.Location())
	}
	if got.Status != StatusTimeout {
		t.Fatalf("旧数据应判为超时，实际 %s", got.Status)
	}
}
