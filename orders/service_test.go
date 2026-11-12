package orders_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bazaar-minggu/db"
	"bazaar-minggu/orders"
)

const eventID = "BAZAR-2026-11-15"

func setup(t *testing.T) (*orders.Service, *db.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx := context.Background()
	if err := d.CreateEvent(ctx, db.Event{
		ID:        eventID,
		Name:      "Bazar KPKM",
		EventDate: "2026-11-15",
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if err := d.SetActiveEvent(ctx, eventID); err != nil {
		t.Fatalf("activate event: %v", err)
	}

	rows := []db.MenuRow{
		{Category: "Makanan", Name: "Sate babi 5 tusuk", Price: 43000, Aliases: []string{"sate", "satay babi"}},
		{Category: "Makanan", Name: "Nasi bungkus babi krengsengan", Price: 18000, Aliases: []string{"nasi krengseng", "babi krengseng"}},
		{Category: "Cemilan", Name: "Kue lumpur bakar Sidoarjo rasa original (5 pcs)", Price: 20000, Aliases: []string{"kue lumpur ori"}},
		{Category: "Minuman", Name: "Cincau Station Orenji", Price: 12000, Aliases: []string{"orenji", "orange cincau"}},
	}
	if _, err := d.SeedProducts(ctx, rows); err != nil {
		t.Fatalf("seed menu: %v", err)
	}
	return orders.New(d), d
}

func productID(t *testing.T, d *db.DB, name string) int64 {
	t.Helper()
	menu, err := d.ListMenu(context.Background(), eventID)
	if err != nil {
		t.Fatalf("list menu: %v", err)
	}
	for _, p := range menu {
		if p.Name == name {
			return p.ID
		}
	}
	t.Fatalf("product %q not found", name)
	return 0
}

func createDraft(t *testing.T, svc *orders.Service, customer string, items ...orders.DraftItemInput) *orders.DraftView {
	t.Helper()
	draft, err := svc.CreateOrderDraft(context.Background(), "", customer, nil, false, items, "")
	if err != nil {
		t.Fatalf("CreateOrderDraft: %v", err)
	}
	return draft
}

func commit(t *testing.T, svc *orders.Service, draftCode string) *orders.OrderView {
	t.Helper()
	order, err := svc.CommitDraft(context.Background(), draftCode)
	if err != nil {
		t.Fatalf("CommitDraft: %v", err)
	}
	return order
}

func errorCode(t *testing.T, err error) string {
	t.Helper()
	var oe *orders.Error
	if !errors.As(err, &oe) {
		t.Fatalf("expected *orders.Error, got %v", err)
	}
	return oe.Code
}

func TestCreateCommitAndIdempotent(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")
	nasi := productID(t, d, "Nasi bungkus babi krengsengan")

	draft := createDraft(t, svc, "Mas Antok",
		orders.DraftItemInput{ProductID: sate, Quantity: 1},
		orders.DraftItemInput{ProductID: nasi, Quantity: 1},
	)
	if draft.Total != 61000 {
		t.Fatalf("draft total = %d, want 61000", draft.Total)
	}
	if !strings.Contains(draft.Preview, "Rp61.000") {
		t.Errorf("preview missing total: %q", draft.Preview)
	}
	if !strings.Contains(draft.Preview, "OK "+draft.ShortCode) {
		t.Errorf("preview missing confirm line: %q", draft.Preview)
	}

	order := commit(t, svc, draft.ShortCode)
	if order.Order.Code != "#BK-001" {
		t.Errorf("code = %q, want #BK-001", order.Order.Code)
	}
	if order.Order.Total != 61000 {
		t.Errorf("order total = %d, want 61000", order.Order.Total)
	}
	if len(order.Order.Items) != 2 {
		t.Errorf("items = %d, want 2", len(order.Order.Items))
	}

	again := commit(t, svc, draft.ShortCode)
	if again.Order.ID != order.Order.ID {
		t.Errorf("idempotent commit created another order: %d vs %d", again.Order.ID, order.Order.ID)
	}

	var audits int
	if err := d.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM order_audit_logs WHERE order_id = ? AND action = 'created'`, order.Order.ID).
		Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Errorf("created audits = %d, want 1", audits)
	}
}

func TestExistingOrder(t *testing.T) {
	svc, d := setup(t)
	sate := productID(t, d, "Sate babi 5 tusuk")

	first := createDraft(t, svc, "Mas Antok", orders.DraftItemInput{ProductID: sate, Quantity: 1})
	commit(t, svc, first.ShortCode)

	_, err := svc.CreateOrderDraft(context.Background(), "", "Antok", nil, false,
		[]orders.DraftItemInput{{ProductID: sate, Quantity: 1}}, "")
	if code := errorCode(t, err); code != "existing_order" {
		t.Fatalf("code = %q, want existing_order", code)
	}
}

func TestEditAndPaymentReset(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")

	order := commit(t, svc, createDraft(t, svc, "Bu Ratna", orders.DraftItemInput{ProductID: sate, Quantity: 1}).ShortCode)
	if _, err := svc.MarkOrderPaid(ctx, order.Order.ID, true); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	edit, err := svc.CreateEditDraft(ctx, order.Order.ID,
		[]orders.DraftItemInput{{ProductID: sate, Quantity: 2}}, "")
	if err != nil {
		t.Fatalf("CreateEditDraft: %v", err)
	}
	if !strings.Contains(edit.Preview, "sudah lunas") {
		t.Errorf("edit preview missing paid warning: %q", edit.Preview)
	}
	if edit.Total != 86000 {
		t.Errorf("edit total = %d, want 86000", edit.Total)
	}

	updated := commit(t, svc, edit.ShortCode)
	if updated.Order.Total != 86000 {
		t.Errorf("updated total = %d, want 86000", updated.Order.Total)
	}
	if updated.Order.IsPaid {
		t.Error("edited order should be unpaid")
	}

	var resets int
	if err := d.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM order_audit_logs WHERE order_id = ? AND action = 'payment_reset'`, order.Order.ID).
		Scan(&resets); err != nil {
		t.Fatal(err)
	}
	if resets != 1 {
		t.Errorf("payment_reset audits = %d, want 1", resets)
	}
}

func TestCancelAndSummary(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")

	order := commit(t, svc, createDraft(t, svc, "Ade", orders.DraftItemInput{ProductID: sate, Quantity: 1}).ShortCode)
	if _, err := svc.MarkOrderPaid(ctx, order.Order.ID, true); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	cancel, err := svc.CreateCancelDraft(ctx, order.Order.ID)
	if err != nil {
		t.Fatalf("CreateCancelDraft: %v", err)
	}
	if !strings.Contains(cancel.Preview, "sudah lunas") {
		t.Errorf("cancel preview missing paid warning: %q", cancel.Preview)
	}
	result := commit(t, svc, cancel.ShortCode)
	if result.Order.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", result.Order.Status)
	}
	if !strings.Contains(result.Formatted, "dibatalkan") {
		t.Errorf("cancel message = %q", result.Formatted)
	}

	summary, err := svc.GetSummary(ctx, eventID, "")
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.Summary.OrderCount != 0 || summary.Summary.Omzet != 0 {
		t.Errorf("summary counts cancelled order: %+v", summary.Summary)
	}
	if summary.Summary.CancelledCount != 1 {
		t.Errorf("cancelled count = %d, want 1", summary.Summary.CancelledCount)
	}
	if summary.Summary.CancelledPaidTotal != 43000 {
		t.Errorf("cancelled paid total = %d, want 43000", summary.Summary.CancelledPaidTotal)
	}

	again, err := svc.CreateCancelDraft(ctx, order.Order.ID)
	if err != nil {
		t.Fatalf("second cancel: %v", err)
	}
	if !again.AlreadyCancelled {
		t.Error("second cancel should report already cancelled")
	}
}

func TestSummaryTotals(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")
	nasi := productID(t, d, "Nasi bungkus babi krengsengan")

	first := commit(t, svc, createDraft(t, svc, "Bu Ratna", orders.DraftItemInput{ProductID: sate, Quantity: 1}).ShortCode)
	if _, err := svc.MarkOrderPaid(ctx, first.Order.ID, true); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	commit(t, svc, createDraft(t, svc, "Ade", orders.DraftItemInput{ProductID: nasi, Quantity: 1}).ShortCode)

	summary, err := svc.GetSummary(ctx, eventID, "")
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.Summary.OrderCount != 2 {
		t.Errorf("order count = %d, want 2", summary.Summary.OrderCount)
	}
	if summary.Summary.Omzet != 61000 {
		t.Errorf("omzet = %d, want 61000", summary.Summary.Omzet)
	}
	if summary.Summary.PaidTotal != 43000 {
		t.Errorf("paid = %d, want 43000", summary.Summary.PaidTotal)
	}
	if summary.Summary.UnpaidTotal != 18000 {
		t.Errorf("unpaid = %d, want 18000", summary.Summary.UnpaidTotal)
	}
	for _, p := range summary.Summary.ProductTotals {
		if p.Name == "Sate babi 5 tusuk" && p.Quantity != 1 {
			t.Errorf("sate qty = %d, want 1", p.Quantity)
		}
	}
	if !strings.Contains(summary.Formatted, "Omzet: Rp61.000") {
		t.Errorf("formatted summary missing omzet: %q", summary.Formatted)
	}
	if !strings.Contains(summary.Formatted, "Per customer:\n\n*#BK-001 — Bu Ratna*") {
		t.Errorf("formatted summary missing customer separator: %q", summary.Formatted)
	}
	if !strings.Contains(summary.Formatted, "Rp43.000 · Lunas") {
		t.Errorf("formatted summary missing paid total line: %q", summary.Formatted)
	}
	if !strings.Contains(summary.Formatted, "*#BK-002 — Ade*") {
		t.Errorf("formatted summary missing second customer: %q", summary.Formatted)
	}
	if !strings.Contains(summary.Formatted, "Rp18.000 · Belum") {
		t.Errorf("formatted summary missing unpaid total line: %q", summary.Formatted)
	}
}

func TestSoldOut(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")

	if _, err := svc.SetProductAvailable(ctx, sate, false, ""); err != nil {
		t.Fatalf("set sold out: %v", err)
	}
	_, err := svc.CreateOrderDraft(ctx, "", "Bu Ratna", nil, false,
		[]orders.DraftItemInput{{ProductID: sate, Quantity: 1}}, "")
	if code := errorCode(t, err); code != "sold_out" {
		t.Fatalf("code = %q, want sold_out", code)
	}

	if _, err := svc.SetProductAvailable(ctx, sate, true, ""); err != nil {
		t.Fatalf("set available: %v", err)
	}
	createDraft(t, svc, "Bu Ratna", orders.DraftItemInput{ProductID: sate, Quantity: 1})
}

func TestDraftExpiry(t *testing.T) {
	svc, d := setup(t)
	sate := productID(t, d, "Sate babi 5 tusuk")
	draft := createDraft(t, svc, "Bu Ratna", orders.DraftItemInput{ProductID: sate, Quantity: 1})

	if _, err := d.SQL().Exec(`UPDATE drafts SET expires_at = ? WHERE id = ?`,
		time.Now().Add(-time.Minute).Unix(), draft.DraftID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CommitDraft(context.Background(), draft.ShortCode)
	if code := errorCode(t, err); code != "draft_expired" {
		t.Fatalf("code = %q, want draft_expired", code)
	}
}

func TestCustomerNearMatch(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")

	commit(t, svc, createDraft(t, svc, "Susi kantor", orders.DraftItemInput{ProductID: sate, Quantity: 1}).ShortCode)

	_, err := svc.CreateOrderDraft(ctx, "", "Susi", nil, false,
		[]orders.DraftItemInput{{ProductID: sate, Quantity: 1}}, "")
	if code := errorCode(t, err); code != "ambiguous_customer" {
		t.Fatalf("code = %q, want ambiguous_customer", code)
	}

	draft, err := svc.CreateOrderDraft(ctx, "", "Susi", nil, true,
		[]orders.DraftItemInput{{ProductID: sate, Quantity: 1}}, "")
	if err != nil {
		t.Fatalf("new customer draft: %v", err)
	}
	order := commit(t, svc, draft.ShortCode)
	if order.Order.CustomerName != "Susi" {
		t.Errorf("customer = %q, want Susi", order.Order.CustomerName)
	}
}

func TestEventIsolation(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")

	commit(t, svc, createDraft(t, svc, "Bu Ratna", orders.DraftItemInput{ProductID: sate, Quantity: 1}).ShortCode)

	if _, err := svc.CreateEvent(ctx, "Bazar Test", "2026-12-20", "", ""); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if _, err := svc.SetActiveEvent(ctx, "EVENT-2026-12-20"); err != nil {
		t.Fatalf("SetActiveEvent: %v", err)
	}
	commit(t, svc, createDraft(t, svc, "Budi", orders.DraftItemInput{ProductID: sate, Quantity: 2}).ShortCode)

	first, err := svc.GetSummary(ctx, eventID, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.GetSummary(ctx, "EVENT-2026-12-20", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary.OrderCount != 1 || first.Summary.Omzet != 43000 {
		t.Errorf("first event summary = %+v", first.Summary)
	}
	if second.Summary.OrderCount != 1 || second.Summary.Omzet != 86000 {
		t.Errorf("second event summary = %+v", second.Summary)
	}
}

func TestPriceSnapshot(t *testing.T) {
	svc, d := setup(t)
	sate := productID(t, d, "Sate babi 5 tusuk")
	draft := createDraft(t, svc, "Bu Ratna", orders.DraftItemInput{ProductID: sate, Quantity: 1})

	if _, err := d.SQL().Exec(`UPDATE products SET price = 50000 WHERE id = ?`, sate); err != nil {
		t.Fatal(err)
	}
	order := commit(t, svc, draft.ShortCode)
	if order.Order.Total != 43000 {
		t.Errorf("snapshot total = %d, want 43000", order.Order.Total)
	}
	if order.Order.Items[0].UnitPrice != 43000 {
		t.Errorf("snapshot unit price = %d, want 43000", order.Order.Items[0].UnitPrice)
	}
}

func TestFindProductAliasAndSearch(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")

	matches, err := svc.FindProduct(ctx, "krengseng", eventID)
	if err != nil {
		t.Fatalf("FindProduct: %v", err)
	}
	if len(matches.Products) != 1 || matches.Products[0].Name != "Nasi bungkus babi krengsengan" {
		t.Errorf("alias match = %+v", matches.Products)
	}

	order := commit(t, svc, createDraft(t, svc, "Bu Ratna", orders.DraftItemInput{ProductID: sate, Quantity: 1}).ShortCode)

	byCustomer, err := svc.SearchOrders(ctx, eventID, "ratna")
	if err != nil || len(byCustomer.Orders) != 1 {
		t.Fatalf("search by customer = %+v, err %v", byCustomer, err)
	}
	byProduct, err := svc.SearchOrders(ctx, eventID, "sate")
	if err != nil || len(byProduct.Orders) != 1 {
		t.Fatalf("search by product = %+v, err %v", byProduct, err)
	}
	byCode, err := svc.SearchOrders(ctx, eventID, "BK-001")
	if err != nil || len(byCode.Orders) != 1 {
		t.Fatalf("search by code = %+v, err %v", byCode, err)
	}

	got, err := svc.GetOrder(ctx, eventID, "#BK-001")
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if got.Order.ID != order.Order.ID {
		t.Errorf("GetOrder id = %d, want %d", got.Order.ID, order.Order.ID)
	}
}
