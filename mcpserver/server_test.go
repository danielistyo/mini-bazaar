package mcpserver_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"bazaar-minggu/db"
	"bazaar-minggu/mcpserver"
	"bazaar-minggu/orders"
)

func setup(t *testing.T) (*db.DB, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx := context.Background()
	eventID := "BAZAR-2026-11-15"
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
	if _, err := d.SeedProducts(ctx, eventID, []db.MenuRow{
		{Category: "Makanan", Name: "Sate babi 5 tusuk", Price: 43000, Aliases: []string{"sate"}},
	}); err != nil {
		t.Fatalf("seed menu: %v", err)
	}
	return d, eventID
}

func connect(t *testing.T, svc *orders.Service) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcpserver.New(svc)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		<-serverDone
	})
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("tool %s returned error: %+v", name, res.Content)
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content for %s = %T", name, res.StructuredContent)
	}
	return m
}

func TestToolsListAndOrderFlow(t *testing.T) {
	d, _ := setup(t)
	session := connect(t, orders.New(d))

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 17 {
		t.Fatalf("tools = %d, want 17", len(tools.Tools))
	}

	events := call(t, session, "list_events", map[string]any{})
	if events["ok"] != true {
		t.Fatalf("list_events = %+v", events)
	}

	menu := call(t, session, "get_menu", map[string]any{})
	data, ok := menu["data"].(map[string]any)
	if !ok {
		t.Fatalf("get_menu data = %T", menu["data"])
	}
	products, ok := data["products"].([]any)
	if !ok || len(products) == 0 {
		t.Fatalf("menu products = %+v", data["products"])
	}
	first := products[0].(map[string]any)
	productID := int64(first["id"].(float64))

	draft := call(t, session, "create_order_draft", map[string]any{
		"customer_name": "Bu Ratna",
		"items": []any{
			map[string]any{"product_id": productID, "quantity": 1},
		},
	})
	if draft["ok"] != true {
		t.Fatalf("create_order_draft = %+v", draft)
	}
	formatted, _ := draft["formatted"].(string)
	if !strings.Contains(formatted, "OK ") {
		t.Fatalf("preview missing OK code: %q", formatted)
	}
	draftData := draft["data"].(map[string]any)
	draftCode, _ := draftData["short_code"].(string)
	if draftCode == "" {
		t.Fatalf("missing short code: %+v", draftData)
	}

	committed := call(t, session, "commit_draft", map[string]any{"draft_code": draftCode})
	if committed["ok"] != true {
		t.Fatalf("commit_draft = %+v", committed)
	}
	receipt, _ := committed["formatted"].(string)
	if !strings.Contains(receipt, "#BK-001") {
		t.Fatalf("receipt = %q", receipt)
	}

	summary := call(t, session, "get_summary", map[string]any{})
	summaryData := summary["data"].(map[string]any)
	summaryObj := summaryData["summary"].(map[string]any)
	if summaryObj["omzet"].(float64) != 43000 {
		t.Fatalf("omzet = %v, want 43000", summaryObj["omzet"])
	}
}

func TestMenuAdminTools(t *testing.T) {
	d, _ := setup(t)
	session := connect(t, orders.New(d))

	added := call(t, session, "upsert_product", map[string]any{
		"name":     "Lauk ayam mboksiah",
		"category": "Makanan",
		"price":    20000,
		"aliases":  []any{"lauk mboksiah"},
	})
	if added["ok"] != true {
		t.Fatalf("upsert_product add = %+v", added)
	}
	product := added["data"].(map[string]any)["product"].(map[string]any)
	productID := int64(product["id"].(float64))
	if product["price"].(float64) != 20000 {
		t.Fatalf("added price = %v", product["price"])
	}
	if formatted, _ := added["formatted"].(string); !strings.Contains(formatted, "ditambahkan") {
		t.Fatalf("add formatted = %q", formatted)
	}

	updated := call(t, session, "upsert_product", map[string]any{
		"product_id": productID,
		"name":       "Lauk ayam mboksiah",
		"category":   "Makanan",
		"price":      21000,
	})
	updatedProduct := updated["data"].(map[string]any)["product"].(map[string]any)
	if updatedProduct["price"].(float64) != 21000 {
		t.Fatalf("updated price = %v", updatedProduct["price"])
	}

	aliasMatch := call(t, session, "find_product", map[string]any{"search_term": "lauk mboksiah"})
	aliasProducts := aliasMatch["data"].(map[string]any)["products"].([]any)
	if len(aliasProducts) != 1 {
		t.Fatalf("alias lookup = %+v", aliasProducts)
	}

	disabled := call(t, session, "set_product_enabled", map[string]any{"product_id": productID, "enabled": false})
	if disabled["ok"] != true {
		t.Fatalf("set_product_enabled = %+v", disabled)
	}
	disabledProduct := disabled["data"].(map[string]any)["product"].(map[string]any)
	if disabledProduct["is_available"] != false {
		t.Fatalf("disabled product = %+v", disabledProduct)
	}

	menu := call(t, session, "get_menu", map[string]any{})
	for _, p := range menu["data"].(map[string]any)["products"].([]any) {
		if int64(p.(map[string]any)["id"].(float64)) == productID {
			t.Fatal("disabled product is still in get_menu")
		}
	}

	conflict := call(t, session, "upsert_product", map[string]any{
		"name":     "Sate babi 5 tusuk",
		"category": "Makanan",
		"price":    43000,
	})
	if conflict["ok"] != false || conflict["code"] != "conflict" {
		t.Fatalf("duplicate upsert = %+v", conflict)
	}

	invalid := call(t, session, "upsert_product", map[string]any{
		"name":     "Produk Test",
		"category": "Makanan",
		"price":    -1,
	})
	if invalid["ok"] != false || invalid["code"] != "invalid_input" {
		t.Fatalf("negative price = %+v", invalid)
	}
}

func TestToolErrorIsStructured(t *testing.T) {
	d, _ := setup(t)
	session := connect(t, orders.New(d))

	res := call(t, session, "find_customer", map[string]any{"name": ""})
	if res["ok"] != false {
		t.Fatalf("expected ok=false, got %+v", res)
	}
	code, _ := res["code"].(string)
	if code != "invalid_input" {
		t.Fatalf("code = %q, want invalid_input", code)
	}
}
