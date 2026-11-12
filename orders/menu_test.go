package orders_test

import (
	"context"
	"strings"
	"testing"

	"bazaar-minggu/orders"
)

func menuPrice(t *testing.T, svc *orders.Service, event, name string) int64 {
	t.Helper()
	menu, err := svc.GetMenu(context.Background(), event)
	if err != nil {
		t.Fatalf("GetMenu(%s): %v", event, err)
	}
	for _, p := range menu.Products {
		if p.Name == name {
			return p.Price
		}
	}
	t.Fatalf("product %q not found in event %s", name, event)
	return 0
}

func productIDFor(t *testing.T, svc *orders.Service, event, term string) int64 {
	t.Helper()
	matches, err := svc.FindProduct(context.Background(), term, event)
	if err != nil {
		t.Fatalf("FindProduct(%s): %v", term, err)
	}
	if len(matches.Products) != 1 {
		t.Fatalf("FindProduct(%s) = %+v, want 1", term, matches.Products)
	}
	return matches.Products[0].ID
}

func TestUpsertProductCreateUpdateConflict(t *testing.T) {
	svc, _ := setup(t)
	ctx := context.Background()

	aliases := []string{"lauk mboksiah", "lauk mbok siah"}
	created, err := svc.UpsertProduct(ctx, eventID, 0, "Lauk ayam mboksiah", "Makanan", 20000, &aliases, nil)
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if created.Product.Price != 20000 || created.Product.Category != "Makanan" {
		t.Fatalf("created product = %+v", created.Product)
	}
	if !strings.Contains(created.Formatted, "ditambahkan ke") || !strings.Contains(created.Formatted, "Bazar KPKM") {
		t.Errorf("formatted create = %q", created.Formatted)
	}

	found, err := svc.FindProduct(ctx, "lauk mboksiah", eventID)
	if err != nil {
		t.Fatalf("find alias: %v", err)
	}
	if len(found.Products) != 1 || found.Products[0].ID != created.Product.ID {
		t.Fatalf("alias match = %+v", found.Products)
	}

	newAliases := []string{"lauk mboksiah", "lauk ayam"}
	updated, err := svc.UpsertProduct(ctx, eventID, created.Product.ID, "Lauk ayam mboksiah", "Makanan", 21000, &newAliases, nil)
	if err != nil {
		t.Fatalf("update product: %v", err)
	}
	if updated.Product.Price != 21000 {
		t.Fatalf("updated price = %d, want 21000", updated.Product.Price)
	}
	if !strings.Contains(updated.Formatted, "diperbarui di") {
		t.Errorf("formatted update = %q", updated.Formatted)
	}

	_, err = svc.UpsertProduct(ctx, eventID, 0, "Lauk Ayam Mboksiah", "Makanan", 22000, nil, nil)
	if code := errorCode(t, err); code != "conflict" {
		t.Fatalf("duplicate code = %q, want conflict", code)
	}

	_, err = svc.UpsertProduct(ctx, eventID, 99999, "Tidak Ada", "Makanan", 1000, nil, nil)
	if code := errorCode(t, err); code != "not_found" {
		t.Fatalf("missing update code = %q, want not_found", code)
	}

	_, err = svc.UpsertProduct(ctx, "EVENT-TIDAK-ADA", 0, "Produk Test", "Makanan", 1000, nil, nil)
	if code := errorCode(t, err); code != "not_found" {
		t.Fatalf("missing event code = %q, want not_found", code)
	}

	tests := []struct {
		name     string
		nameArg  string
		category string
		price    int64
	}{
		{"empty name", "  ", "Makanan", 1000},
		{"empty category", "Produk Test", "  ", 1000},
		{"negative price", "Produk Test", "Makanan", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.UpsertProduct(ctx, eventID, 0, tt.nameArg, tt.category, tt.price, nil, nil)
			if code := errorCode(t, err); code != "invalid_input" {
				t.Fatalf("code = %q, want invalid_input", code)
			}
		})
	}
}

func TestSetProductEnabled(t *testing.T) {
	svc, d := setup(t)
	ctx := context.Background()
	sate := productID(t, d, "Sate babi 5 tusuk")

	disabled, err := svc.SetProductEnabled(ctx, eventID, sate, false)
	if err != nil {
		t.Fatalf("disable product: %v", err)
	}
	if disabled.Product.IsAvailable {
		t.Error("product should be disabled")
	}

	menu, err := svc.GetMenu(ctx, eventID)
	if err != nil {
		t.Fatalf("get menu: %v", err)
	}
	for _, p := range menu.Products {
		if p.ID == sate {
			t.Fatal("disabled product should not appear in the menu")
		}
	}

	matches, err := svc.FindProduct(ctx, "sate", eventID)
	if err != nil {
		t.Fatalf("find disabled product: %v", err)
	}
	if len(matches.Products) == 0 || matches.Products[0].Available {
		t.Fatalf("find_product = %+v, want unavailable", matches.Products)
	}

	_, err = svc.CreateOrderDraft(ctx, eventID, "Bu Ratna", nil, false,
		[]orders.DraftItemInput{{ProductID: sate, Quantity: 1}}, "")
	if code := errorCode(t, err); code != "sold_out" {
		t.Fatalf("disabled order code = %q, want sold_out", code)
	}

	if _, err := svc.SetProductEnabled(ctx, eventID, sate, true); err != nil {
		t.Fatalf("enable product: %v", err)
	}
	menu, err = svc.GetMenu(ctx, eventID)
	if err != nil {
		t.Fatalf("get menu after enable: %v", err)
	}
	found := false
	for _, p := range menu.Products {
		if p.ID == sate {
			found = true
		}
	}
	if !found {
		t.Fatal("enabled product should appear in the menu")
	}

	_, err = svc.SetProductEnabled(ctx, eventID, 99999, true)
	if code := errorCode(t, err); code != "not_found" {
		t.Fatalf("missing product code = %q, want not_found", code)
	}
	_, err = svc.SetProductEnabled(ctx, eventID, 99999, false)
	if code := errorCode(t, err); code != "not_found" {
		t.Fatalf("missing disabled product code = %q, want not_found", code)
	}
}

func TestPerEventMenuIsolation(t *testing.T) {
	svc, _ := setup(t)
	ctx := context.Background()
	const otherEvent = "EVENT-2026-12-20"

	if _, err := svc.CreateEvent(ctx, "Bazar Test", "2026-12-20", "", otherEvent); err != nil {
		t.Fatalf("create event: %v", err)
	}

	if got := menuPrice(t, svc, otherEvent, "Sate babi 5 tusuk"); got != 43000 {
		t.Fatalf("new event sate price = %d, want copied 43000", got)
	}

	aliases := []string{"es teh"}
	created, err := svc.UpsertProduct(ctx, otherEvent, 0, "Es teh manis", "Minuman", 5000, &aliases, nil)
	if err != nil {
		t.Fatalf("add product to second event: %v", err)
	}

	menuA, err := svc.GetMenu(ctx, eventID)
	if err != nil {
		t.Fatalf("menu event A: %v", err)
	}
	for _, p := range menuA.Products {
		if p.ID == created.Product.ID {
			t.Fatal("product added to event B leaked into event A")
		}
	}
	matches, err := svc.FindProduct(ctx, "es teh", eventID)
	if err != nil {
		t.Fatalf("find in event A: %v", err)
	}
	if len(matches.Products) != 0 {
		t.Fatalf("event A found event B product: %+v", matches.Products)
	}
	if got := menuPrice(t, svc, otherEvent, "Es teh manis"); got != 5000 {
		t.Fatalf("event B es teh price = %d, want 5000", got)
	}

	sateA := productIDFor(t, svc, eventID, "sate")
	sateB := productIDFor(t, svc, otherEvent, "sate")
	if sateA != sateB {
		t.Fatalf("catalog product id changed between events: %d vs %d", sateA, sateB)
	}
	if _, err := svc.UpsertProduct(ctx, otherEvent, sateB, "Sate babi 5 tusuk", "Makanan", 50000, nil, nil); err != nil {
		t.Fatalf("reprice sate in event B: %v", err)
	}
	if got := menuPrice(t, svc, eventID, "Sate babi 5 tusuk"); got != 43000 {
		t.Fatalf("event A sate price = %d, want 43000", got)
	}
	if got := menuPrice(t, svc, otherEvent, "Sate babi 5 tusuk"); got != 50000 {
		t.Fatalf("event B sate price = %d, want 50000", got)
	}

	draftA := draftForEvent(t, svc, eventID, "Bu Ratna",
		orders.DraftItemInput{ProductID: sateA, Quantity: 1})
	if draftA.Total != 43000 {
		t.Fatalf("event A draft total = %d, want 43000", draftA.Total)
	}
	draftB := draftForEvent(t, svc, otherEvent, "Bu Ratna",
		orders.DraftItemInput{ProductID: sateB, Quantity: 1})
	if draftB.Total != 50000 {
		t.Fatalf("event B draft total = %d, want 50000", draftB.Total)
	}
	orderA := commit(t, svc, draftA.ShortCode)
	orderB := commit(t, svc, draftB.ShortCode)
	if orderA.Order.Total != 43000 || orderB.Order.Total != 50000 {
		t.Fatalf("order totals = %d / %d, want 43000 / 50000", orderA.Order.Total, orderB.Order.Total)
	}

	if _, err := svc.SetProductEnabled(ctx, otherEvent, sateB, false); err != nil {
		t.Fatalf("disable sate in event B: %v", err)
	}
	if got := menuPrice(t, svc, eventID, "Sate babi 5 tusuk"); got != 43000 {
		t.Fatalf("event A sate disappeared: %d", got)
	}
	_, err = svc.CreateOrderDraft(ctx, otherEvent, "Budi", nil, false,
		[]orders.DraftItemInput{{ProductID: sateB, Quantity: 1}}, "")
	if code := errorCode(t, err); code != "sold_out" {
		t.Fatalf("disabled event B order code = %q, want sold_out", code)
	}

	if _, err := svc.SetProductEnabled(ctx, eventID, created.Product.ID, true); err != nil {
		t.Fatalf("attach existing catalog product to event A: %v", err)
	}
	if got := menuPrice(t, svc, eventID, "Es teh manis"); got != 5000 {
		t.Fatalf("attached product price = %d, want catalog 5000", got)
	}
}
