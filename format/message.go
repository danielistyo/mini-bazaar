package format

import (
	"fmt"
	"strings"

	"bazaar-minggu/db"
)

func eventTitle(e db.Event) string {
	return fmt.Sprintf("%s — %s", e.Name, EventDate(e.EventDate))
}

func payLabel(paid bool) string {
	if paid {
		return "Lunas"
	}
	return "Belum bayar"
}

func statusLabel(status string) string {
	if status == "cancelled" {
		return "Dibatalkan"
	}
	return "Aktif"
}

func itemLine(name string, qty int, unit int64, note string) string {
	line := fmt.Sprintf("• %s × %d — %s × %d = %s", name, qty, Rupiah(unit), qty, Rupiah(int64(qty)*unit))
	if note != "" {
		line += " (" + note + ")"
	}
	return line
}

func orderItemLines(items []db.OrderItemDetail) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, itemLine(item.ProductName, item.Quantity, item.UnitPrice, item.Note))
	}
	return lines
}

func draftItemLines(items []db.DraftItem) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, itemLine(item.Name, item.Quantity, item.UnitPrice, item.Note))
	}
	return lines
}

// OrderPreview is the confirmation message for a create draft.
func OrderPreview(e *db.Event, customer string, items []db.DraftItem, total int64, code string) string {
	var b strings.Builder
	b.WriteString("🧾 Draft order\n")
	fmt.Fprintf(&b, "%s\n", eventTitle(*e))
	fmt.Fprintf(&b, "Customer: %s\n\n", customer)
	for _, line := range draftItemLines(items) {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\nTotal: %s\n", Rupiah(total))
	fmt.Fprintf(&b, "Balas: OK %s\n", code)
	return b.String()
}

// EditPreview shows before and after for an edit draft.
func EditPreview(order *db.OrderDetail, items []db.DraftItem, total int64, code string, resetPaid bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✏️ Edit order %s\n", order.Code)
	fmt.Fprintf(&b, "%s\n", eventTitle(db.Event{Name: order.EventName, EventDate: order.EventDate}))
	fmt.Fprintf(&b, "Customer: %s\n\n", order.CustomerName)
	b.WriteString("Sebelum:\n")
	for _, line := range orderItemLines(order.Items) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\nSesudah:\n")
	for _, line := range draftItemLines(items) {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\nTotal baru: %s\n", Rupiah(total))
	if resetPaid {
		b.WriteString("⚠️ Order sudah lunas. Status pembayaran akan kembali Belum bayar.\n")
	}
	fmt.Fprintf(&b, "Balas: OK %s\n", code)
	return b.String()
}

// CancelPreview is the confirmation message for a cancel draft.
func CancelPreview(order *db.OrderDetail, code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ Cancel order %s\n", order.Code)
	fmt.Fprintf(&b, "%s\n", eventTitle(db.Event{Name: order.EventName, EventDate: order.EventDate}))
	fmt.Fprintf(&b, "Customer: %s\n\n", order.CustomerName)
	for _, line := range orderItemLines(order.Items) {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\nTotal: %s\n", Rupiah(order.Total))
	fmt.Fprintf(&b, "Pembayaran: %s\n", payLabel(order.IsPaid))
	if order.IsPaid {
		b.WriteString("⚠️ Order sudah lunas. Refund ditangani manual.\n")
	}
	fmt.Fprintf(&b, "Balas: OK %s\n", code)
	return b.String()
}

// Receipt is the message after a successful draft commit.
func Receipt(order *db.OrderDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✅ Order %s tersimpan\n", order.Code)
	fmt.Fprintf(&b, "%s\n", eventTitle(db.Event{Name: order.EventName, EventDate: order.EventDate}))
	fmt.Fprintf(&b, "Customer: %s\n\n", order.CustomerName)
	for _, line := range orderItemLines(order.Items) {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\nTotal: %s\n", Rupiah(order.Total))
	fmt.Fprintf(&b, "Pembayaran: %s\n", payLabel(order.IsPaid))
	return b.String()
}

// PaidMessage reports a payment status change.
func PaidMessage(order *db.OrderDetail, paid bool) string {
	if paid {
		return fmt.Sprintf("✅ %s ditandai Lunas — %s.", order.Code, Rupiah(order.Total))
	}
	return fmt.Sprintf("↩️ %s ditandai Belum bayar — %s.", order.Code, Rupiah(order.Total))
}

// CommitMessage reports a committed draft based on its kind.
func CommitMessage(order *db.OrderDetail, kind string) string {
	switch kind {
	case "edit":
		var b strings.Builder
		fmt.Fprintf(&b, "✏️ Order %s diperbarui\n", order.Code)
		fmt.Fprintf(&b, "Customer: %s\n\n", order.CustomerName)
		for _, line := range orderItemLines(order.Items) {
			b.WriteString(line + "\n")
		}
		fmt.Fprintf(&b, "\nTotal: %s\n", Rupiah(order.Total))
		fmt.Fprintf(&b, "Pembayaran: %s\n", payLabel(order.IsPaid))
		return b.String()
	case "cancel":
		var b strings.Builder
		fmt.Fprintf(&b, "🗑️ Order %s dibatalkan\n", order.Code)
		fmt.Fprintf(&b, "Customer: %s\n", order.CustomerName)
		fmt.Fprintf(&b, "Total: %s\n", Rupiah(order.Total))
		fmt.Fprintf(&b, "Pembayaran: %s\n", payLabel(order.IsPaid))
		if order.IsPaid {
			b.WriteString("⚠️ Order sudah lunas. Refund ditangani manual.\n")
		}
		return b.String()
	default:
		return Receipt(order)
	}
}

// Summary renders the event report.
func Summary(s *db.Summary, customer string) string {
	var b strings.Builder
	title := eventTitle(s.Event)
	if customer != "" {
		title = "Ringkasan " + customer + " — " + title
	}
	fmt.Fprintf(&b, "📊 %s\n", title)
	fmt.Fprintf(&b, "Order: %d | Omzet: %s | Lunas: %s | Belum: %s\n", s.OrderCount, Rupiah(s.Omzet), Rupiah(s.PaidTotal), Rupiah(s.UnpaidTotal))

	b.WriteString("\nPer customer:\n\n")
	if len(s.Orders) == 0 {
		b.WriteString("• Belum ada order.\n\n")
	}
	for _, o := range s.Orders {
		fmt.Fprintf(&b, "*%s — %s*\n", o.Code, o.CustomerName)
		fmt.Fprintf(&b, "%s · %s\n", Rupiah(o.Total), payLabel(o.IsPaid))
		for _, item := range o.Items {
			b.WriteString(itemLine(item.ProductName, item.Quantity, item.UnitPrice, item.Note) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Per produk:\n")
	if len(s.ProductTotals) == 0 {
		b.WriteString("• Belum ada item.\n")
	}
	for _, p := range s.ProductTotals {
		fmt.Fprintf(&b, "• %s: %d\n", p.Name, p.Quantity)
	}

	if s.CancelledCount > 0 {
		fmt.Fprintf(&b, "\nDibatalkan: %d order", s.CancelledCount)
		if s.CancelledPaidTotal > 0 {
			fmt.Fprintf(&b, " (lunas %s, refund manual)", Rupiah(s.CancelledPaidTotal))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Menu renders the menu grouped by category.
func Menu(e *db.Event, products []db.MenuProduct) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🍽️ Menu %s\n", eventTitle(*e))
	last := ""
	for _, p := range products {
		if p.Category != last {
			last = p.Category
			fmt.Fprintf(&b, "\n%s:\n", last)
		}
		line := fmt.Sprintf("• %s — %s", p.Name, Rupiah(p.Price))
		if !p.Available {
			line += " (habis)"
		}
		b.WriteString(line + "\n")
	}
	if len(products) == 0 {
		b.WriteString("\nMenu belum di-seed.\n")
	}
	return b.String()
}

// Events renders the event list.
func Events(events []db.EventView) string {
	if len(events) == 0 {
		return "Belum ada event."
	}
	var b strings.Builder
	b.WriteString("📅 Event:\n")
	for _, e := range events {
		active := ""
		if e.IsActive {
			active = " (aktif)"
		}
		fmt.Fprintf(&b, "• %s — %s, %s%s — %d order, %s\n",
			e.ID, e.Name, EventDate(e.EventDate), active, e.OrderCount, Rupiah(e.Omzet))
	}
	return b.String()
}

// ProductMatches renders product lookup candidates.
func ProductMatches(matches []db.ProductMatch) string {
	if len(matches) == 0 {
		return "Produk tidak ditemukan."
	}
	var b strings.Builder
	b.WriteString("Kandidat produk:\n")
	for _, p := range matches {
		line := fmt.Sprintf("• %s — %s", p.Name, Rupiah(p.Price))
		if !p.Available {
			line += " (habis)"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// CustomerMatches renders customer lookup candidates.
func CustomerMatches(matches []db.CustomerMatch) string {
	if len(matches) == 0 {
		return "Tidak ada customer dengan nama itu. Ini customer baru?"
	}
	var b strings.Builder
	b.WriteString("Kandidat customer:\n")
	for _, c := range matches {
		fmt.Fprintf(&b, "• %s (%d order)\n", c.DisplayName, c.OrderCount)
	}
	return b.String()
}

// OrderDetailText renders one order.
func OrderDetailText(order *db.OrderDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n", order.Code, order.CustomerName)
	fmt.Fprintf(&b, "%s\n", eventTitle(db.Event{Name: order.EventName, EventDate: order.EventDate}))
	fmt.Fprintf(&b, "Status: %s | Pembayaran: %s\n\n", statusLabel(order.Status), payLabel(order.IsPaid))
	for _, line := range orderItemLines(order.Items) {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\nTotal: %s\n", Rupiah(order.Total))
	return b.String()
}

// SearchResults renders compact order search results.
func SearchResults(rows []db.OrderRow) string {
	if len(rows) == 0 {
		return "Tidak ada order yang cocok."
	}
	var b strings.Builder
	b.WriteString("Hasil pencarian:\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "• %s %s — %s — %s — %s\n",
			row.Code, row.CustomerName, Rupiah(row.Total), payLabel(row.IsPaid), statusLabel(row.Status))
	}
	return b.String()
}

// EventCreated reports a new inactive event.
func EventCreated(e *db.Event) string {
	return fmt.Sprintf("✅ Event %s (%s, %s) dibuat. Belum aktif. Balas \"aktifkan event %s\" untuk memakainya.",
		e.ID, e.Name, EventDate(e.EventDate), e.ID)
}

// EventActivated reports the active event.
func EventActivated(e *db.Event) string {
	return fmt.Sprintf("✅ Event aktif: %s — %s, %s.", e.ID, e.Name, EventDate(e.EventDate))
}

// ProductAvailability reports a sold-out toggle.
func ProductAvailability(e *db.Event, p *db.MenuProduct, available bool) string {
	state := "habis"
	if available {
		state = "tersedia lagi"
	}
	return fmt.Sprintf("✅ %s ditandai %s untuk %s.", p.Name, state, e.Name)
}
