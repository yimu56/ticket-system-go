package server

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"ticketsystem/internal/store"
)

func urlEnc(s string) string { return url.PathEscape(s) }

func typeName(p int) string {
	if p >= 1 && p <= len(store.TypeNames) {
		return store.TypeNames[p-1]
	}
	return "未知"
}

func yn(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "无"
	}
	return s
}

// writeTXT 输出与 Python 版 _export_to_txt 相同的版式
func writeTXT(w io.Writer, tickets []*store.Ticket) error {
	var b strings.Builder
	fmt.Fprintf(&b, "工单导出时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "共导出 %d 个工单\n", len(tickets))
	b.WriteString(strings.Repeat("=", 50) + "\n\n")

	for i, t := range tickets {
		fmt.Fprintf(&b, "工单号: %s\n", t.TicketID)
		fmt.Fprintf(&b, "客户名称: %s\n", t.ClientName)
		fmt.Fprintf(&b, "联系人: %s\n", orDash(t.ContactPerson))
		fmt.Fprintf(&b, "联系方式: %s\n", orDash(t.ContactPhone))
		fmt.Fprintf(&b, "项目类型: %s\n", typeName(t.Priority))
		fmt.Fprintf(&b, "问题描述:\n%s\n", t.Description)
		fmt.Fprintf(&b, "处理记录:\n%s\n", orDash(t.Record))
		fmt.Fprintf(&b, "是否上升: %s\n", yn(t.Rise))
		fmt.Fprintf(&b, "是否闭环: %s\n", yn(t.Closed))
		fmt.Fprintf(&b, "创建时间: %s\n", t.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(&b, "最后更新时间: %s\n", t.UpdatedAt.Format("2006-01-02 15:04:05"))
		if i < len(tickets)-1 {
			b.WriteString("\n" + strings.Repeat("-", 40) + "\n\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeCSV 输出 UTF-8 CSV（调用方已写 BOM）
func writeCSV(w io.Writer, tickets []*store.Ticket) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{"工单号", "客户名称", "联系人", "联系方式", "项目类型",
		"问题描述", "处理记录", "是否上升", "是否闭环", "创建时间", "最后更新时间"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, t := range tickets {
		row := []string{
			t.TicketID, t.ClientName, t.ContactPerson, t.ContactPhone,
			typeName(t.Priority), t.Description, t.Record, yn(t.Rise), yn(t.Closed),
			t.CreatedAt.Format("2006-01-02 15:04:05"),
			t.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	return cw.Error()
}
