package orders_test

import (
	"context"
	"strings"
	"testing"

	"bazaar-minggu/orders"
)

func draftForEvent(t *testing.T, svc *orders.Service, eventID, customer string, items ...orders.DraftItemInput) *orders.DraftView {
	t.Helper()
	draft, err := svc.CreateOrderDraft(context.Background(), eventID, customer, nil, false, items, "")
	if err != nil {
		t.Fatalf("CreateOrderDraft(%s): %v", customer, err)
	}
	return draft
}

func TestEndToEndBazaarFlow(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	const eventA = "BAZAR-2026-11-15"
	const eventB = "EVENT-2026-12-20"

	// Menu admin: add a product, update it, disable it, reject an order, re-enable it.
	aliases := []string{"lauk mboksiah"}
	lauk, err := svc.UpsertProduct(ctx, eventA, 0, "Lauk ayam mboksiah", "Makanan", 20000, &aliases, nil)
	if err != nil {
		t.Fatalf("add menu product: %v", err)
	}
	laukID := lauk.Product.ID

	newAliases := []string{"lauk mboksiah", "lauk ayam"}
	if _, err := svc.UpsertProduct(ctx, eventA, laukID, "Lauk ayam mboksiah", "Makanan", 21000, &newAliases, nil); err != nil {
		t.Fatalf("update menu product: %v", err)
	}

	if _, err := svc.SetProductEnabled(ctx, eventA, laukID, false); err != nil {
		t.Fatalf("disable menu product: %v", err)
	}
	menu, err := svc.GetMenu(ctx, eventA)
	if err != nil {
		t.Fatalf("get menu: %v", err)
	}
	for _, p := range menu.Products {
		if p.ID == laukID {
			t.Fatal("disabled product is still in get_menu")
		}
	}
	matches, err := svc.FindProduct(ctx, "lauk mboksiah", eventA)
	if err != nil {
		t.Fatalf("find disabled product: %v", err)
	}
	if len(matches.Products) == 0 || matches.Products[0].Available {
		t.Fatalf("disabled product lookup = %+v", matches.Products)
	}
	_, err = svc.CreateOrderDraft(ctx, eventA, "Bu Ratna", nil, false,
		[]orders.DraftItemInput{{ProductID: laukID, Quantity: 1}}, "")
	if code := errorCode(t, err); code != "sold_out" {
		t.Fatalf("disabled product order code = %q, want sold_out", code)
	}
	if _, err := svc.SetProductEnabled(ctx, eventA, laukID, true); err != nil {
		t.Fatalf("re-enable menu product: %v", err)
	}

	// Event management: create and activate a second event.
	if _, err := svc.CreateEvent(ctx, "Bazar Test", "2026-12-20", "", ""); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := svc.SetActiveEvent(ctx, eventB); err != nil {
		t.Fatalf("activate event: %v", err)
	}

	sate := productID(t, d, "Sate babi 5 tusuk")
	nasi := productID(t, d, "Nasi bungkus babi krengsengan")

	// Order flow: create, edit, pay.
	ratnaDraft := draftForEvent(t, svc, eventB, "Bu Ratna",
		orders.DraftItemInput{ProductID: sate, Quantity: 1},
		orders.DraftItemInput{ProductID: laukID, Quantity: 1},
	)
	if ratnaDraft.Total != 64000 {
		t.Fatalf("draft total = %d, want 64000", ratnaDraft.Total)
	}
	if !strings.Contains(ratnaDraft.Preview, "OK "+ratnaDraft.ShortCode) {
		t.Fatalf("preview missing OK code: %q", ratnaDraft.Preview)
	}
	ratna := commit(t, svc, ratnaDraft.ShortCode)
	if ratna.Order.Code != "#BK-001" || ratna.Order.Total != 64000 {
		t.Fatalf("created order = %+v", ratna.Order)
	}
	again := commit(t, svc, ratnaDraft.ShortCode)
	if again.Order.ID != ratna.Order.ID {
		t.Fatalf("idempotent commit created another order: %d vs %d", again.Order.ID, ratna.Order.ID)
	}

	edit, err := svc.CreateEditDraft(ctx, ratna.Order.ID, []orders.DraftItemInput{
		{ProductID: sate, Quantity: 2},
		{ProductID: laukID, Quantity: 1},
	}, "")
	if err != nil {
		t.Fatalf("create edit draft: %v", err)
	}
	updated := commit(t, svc, edit.ShortCode)
	if updated.Order.Total != 107000 {
		t.Fatalf("edited total = %d, want 107000", updated.Order.Total)
	}
	if _, err := svc.MarkOrderPaid(ctx, updated.Order.ID, true); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	summary, err := svc.GetSummary(ctx, eventB, "")
	if err != nil {
		t.Fatalf("summary after pay: %v", err)
	}
	if summary.Summary.OrderCount != 1 || summary.Summary.Omzet != 107000 ||
		summary.Summary.PaidTotal != 107000 || summary.Summary.UnpaidTotal != 0 {
		t.Fatalf("summary after pay = %+v", summary.Summary)
	}
	if !strings.Contains(summary.Formatted, "*#BK-001 — Bu Ratna*") ||
		!strings.Contains(summary.Formatted, "Rp107.000 · Lunas") {
		t.Fatalf("summary format = %q", summary.Formatted)
	}

	// Second order, then cancel it.
	ade := commit(t, svc, draftForEvent(t, svc, eventB, "Ade",
		orders.DraftItemInput{ProductID: nasi, Quantity: 1}).ShortCode)
	summary, err = svc.GetSummary(ctx, eventB, "")
	if err != nil {
		t.Fatalf("summary with two orders: %v", err)
	}
	if summary.Summary.OrderCount != 2 || summary.Summary.Omzet != 125000 ||
		summary.Summary.PaidTotal != 107000 || summary.Summary.UnpaidTotal != 18000 {
		t.Fatalf("summary with two orders = %+v", summary.Summary)
	}
	cancel, err := svc.CreateCancelDraft(ctx, ade.Order.ID)
	if err != nil {
		t.Fatalf("create cancel draft: %v", err)
	}
	cancelled := commit(t, svc, cancel.ShortCode)
	if cancelled.Order.Status != "cancelled" {
		t.Fatalf("cancelled status = %q", cancelled.Order.Status)
	}
	summary, err = svc.GetSummary(ctx, eventB, "")
	if err != nil {
		t.Fatalf("summary after cancel: %v", err)
	}
	if summary.Summary.OrderCount != 1 || summary.Summary.Omzet != 107000 ||
		summary.Summary.CancelledCount != 1 || summary.Summary.UnpaidTotal != 0 {
		t.Fatalf("summary after cancel = %+v", summary.Summary)
	}

	// Per-event sold out: event B blocks sate, event A still accepts it.
	if _, err := svc.SetProductAvailable(ctx, sate, false, eventB); err != nil {
		t.Fatalf("sold out event B: %v", err)
	}
	_, err = svc.CreateOrderDraft(ctx, eventB, "Budi", nil, false,
		[]orders.DraftItemInput{{ProductID: sate, Quantity: 1}}, "")
	if code := errorCode(t, err); code != "sold_out" {
		t.Fatalf("event B sold out code = %q, want sold_out", code)
	}
	susiDraft := draftForEvent(t, svc, eventA, "Susi",
		orders.DraftItemInput{ProductID: sate, Quantity: 1})
	susi := commit(t, svc, susiDraft.ShortCode)
	if susi.Order.Code != "#BK-001" || susi.Order.Total != 43000 {
		t.Fatalf("event A order = %+v", susi.Order)
	}
	if _, err := svc.SetProductAvailable(ctx, sate, true, eventB); err != nil {
		t.Fatalf("restore sate event B: %v", err)
	}

	summaryA, err := svc.GetSummary(ctx, eventA, "")
	if err != nil {
		t.Fatalf("summary event A: %v", err)
	}
	if summaryA.Summary.OrderCount != 1 || summaryA.Summary.Omzet != 43000 {
		t.Fatalf("summary event A = %+v", summaryA.Summary)
	}
	summaryB, err := svc.GetSummary(ctx, eventB, "")
	if err != nil {
		t.Fatalf("summary event B after event A order: %v", err)
	}
	if summaryB.Summary.OrderCount != 1 || summaryB.Summary.Omzet != 107000 {
		t.Fatalf("summary event B leaked event A: %+v", summaryB.Summary)
	}

	// Price change after order: existing order keeps its snapshot.
	if _, err := svc.UpsertProduct(ctx, eventB, laukID, "Lauk ayam mboksiah", "Makanan", 25000, &newAliases, nil); err != nil {
		t.Fatalf("update price after order: %v", err)
	}
	detail, err := svc.GetOrder(ctx, eventB, "#BK-001")
	if err != nil {
		t.Fatalf("get order after price change: %v", err)
	}
	for _, item := range detail.Order.Items {
		if item.ProductID == laukID && item.UnitPrice != 21000 {
			t.Fatalf("snapshot price = %d, want 21000", item.UnitPrice)
		}
	}

	// Removing from this event hides it but keeps the existing order and summary.
	if _, err := svc.SetProductEnabled(ctx, eventB, laukID, false); err != nil {
		t.Fatalf("disable after order: %v", err)
	}
	summaryB, err = svc.GetSummary(ctx, eventB, "")
	if err != nil {
		t.Fatalf("summary after disable: %v", err)
	}
	if summaryB.Summary.Omzet != 107000 {
		t.Fatalf("disable changed summary: %+v", summaryB.Summary)
	}
	menu, err = svc.GetMenu(ctx, eventB)
	if err != nil {
		t.Fatalf("menu after disable: %v", err)
	}
	for _, p := range menu.Products {
		if p.ID == laukID {
			t.Fatal("disabled product reappeared in the menu")
		}
	}
	if _, err := svc.SetProductEnabled(ctx, eventB, laukID, true); err != nil {
		t.Fatalf("re-enable after order: %v", err)
	}

	// Search by customer, product, and order code.
	for _, keyword := range []string{"ratna", "lauk", "BK-001"} {
		found, err := svc.SearchOrders(ctx, eventB, keyword)
		if err != nil {
			t.Fatalf("search %q: %v", keyword, err)
		}
		if len(found.Orders) == 0 {
			t.Errorf("search %q returned no orders", keyword)
		}
	}
}
