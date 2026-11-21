package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"bazaar-minggu/db"
	"bazaar-minggu/orders"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func addMember(t *testing.T, d *db.DB, channel, ref, role string) {
	t.Helper()
	now := time.Now().Unix()
	res, err := d.SQL().Exec(`INSERT INTO accounts (created_at) VALUES (?)`, now)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("account id: %v", err)
	}
	if _, err := d.SQL().Exec(
		`INSERT INTO account_channels (account_id, channel, ref, status, created_at) VALUES (?, ?, ?, 'active', ?)`,
		accountID, channel, ref, now); err != nil {
		t.Fatalf("channel: %v", err)
	}
	if _, err := d.SQL().Exec(
		`INSERT INTO store_members (store_id, account_id, role, status, created_at, activated_at) VALUES (1, ?, ?, 'active', ?, ?)`,
		accountID, role, now, now); err != nil {
		t.Fatalf("member: %v", err)
	}
}

func requestWith(meta mcp.Meta) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: meta}}
}

func TestReadCallContext(t *testing.T) {
	tests := []struct {
		name string
		req  *mcp.CallToolRequest
		want callContext
	}{
		{"nil request", nil, callContext{}},
		{"no meta", requestWith(nil), callContext{}},
		{"no key", requestWith(mcp.Meta{"other": "x"}), callContext{}},
		{
			"valid",
			requestWith(mcp.Meta{contextMetaKey: map[string]any{
				"channel": "whatsapp", "chat_ref": "c", "sender_ref": "s",
			}}),
			callContext{Channel: "whatsapp", ChatRef: "c", SenderRef: "s"},
		},
		{"wrong shape", requestWith(mcp.Meta{contextMetaKey: "not-an-object"}), callContext{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readCallContext(tt.req); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveWithoutContext(t *testing.T) {
	d := newTestDB(t)
	h := &handlers{db: d, svc: orders.New(d)}

	if _, err := h.resolve(context.Background(), requestWith(nil)); err == nil {
		t.Fatal("want refusal when context is missing")
	} else if oe, ok := err.(*orders.Error); !ok || oe.Code != "no_store" {
		t.Fatalf("err = %v, want no_store", err)
	}
}

func TestResolveWithPilot(t *testing.T) {
	d := newTestDB(t)
	pilot := &db.Scope{StoreID: 1, Role: db.RoleOrganizer, Kind: db.PrincipalAccount}
	h := &handlers{db: d, svc: orders.New(d), pilot: pilot}

	sc, err := h.resolve(context.Background(), requestWith(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sc.StoreID != 1 || sc.Role != db.RoleOrganizer {
		t.Fatalf("scope = %+v", sc)
	}
}

func TestResolveFromMembership(t *testing.T) {
	d := newTestDB(t)
	addMember(t, d, "whatsapp", "628123@s.whatsapp.net", "staff")
	h := &handlers{db: d, svc: orders.New(d)}

	req := requestWith(mcp.Meta{contextMetaKey: map[string]any{
		"channel": "whatsapp", "chat_ref": "628123@s.whatsapp.net", "sender_ref": "628123@s.whatsapp.net",
	}})
	sc, err := h.resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sc.StoreID != 1 || sc.Role != db.RoleStaff {
		t.Fatalf("scope = %+v", sc)
	}

	unknown := requestWith(mcp.Meta{contextMetaKey: map[string]any{
		"channel": "whatsapp", "chat_ref": "x", "sender_ref": "x",
	}})
	if _, err := h.resolve(context.Background(), unknown); err == nil {
		t.Fatal("want refusal for an unknown caller")
	}
}

func TestRoleMatrix(t *testing.T) {
	// Every registered tool must appear in the matrix; anything else is denied.
	registered := []string{
		"list_events", "create_event", "set_active_event", "get_menu", "find_product",
		"set_product_available", "upsert_product", "set_product_enabled", "find_customer",
		"create_order_draft", "create_edit_draft", "create_cancel_draft", "commit_draft",
		"get_order", "search_orders", "mark_order_paid", "get_summary", "create_group_claim",
	}
	for _, name := range registered {
		if _, ok := toolRoles[name]; !ok {
			t.Fatalf("tool %q has no minimum role", name)
		}
	}
	if _, ok := toolRoles["drop_everything"]; ok {
		t.Fatal("unknown tool must not be in the matrix")
	}

	tests := []struct {
		tool  string
		role  db.Role
		allow bool
	}{
		{"create_order_draft", db.RoleStaff, true},
		{"commit_draft", db.RoleStaff, true},
		{"upsert_product", db.RoleStaff, false},
		{"upsert_product", db.RoleOrganizer, true},
		{"create_event", db.RoleStaff, false},
		{"get_summary", db.RoleStaff, true},
	}
	for _, tt := range tests {
		t.Run(tt.tool+"/"+string(tt.role), func(t *testing.T) {
			if got := tt.role.Allows(toolRoles[tt.tool]); got != tt.allow {
				t.Fatalf("allows = %v, want %v", got, tt.allow)
			}
		})
	}
}

func TestScopedHandlerUsesStoreService(t *testing.T) {
	d := newTestDB(t)
	h := &handlers{db: d, svc: orders.New(d)}
	sc := &db.Scope{StoreID: 1, Role: db.RoleStaff, Kind: db.PrincipalGroup}

	got := h.scoped(sc)
	if got.svc == h.svc {
		t.Fatal("scoped handlers must not share the unscoped service")
	}
	if got.scope != sc {
		t.Fatal("scope not attached to handlers")
	}
	if h.scope != nil {
		t.Fatal("receiver was mutated")
	}
}

func TestScopeInContext(t *testing.T) {
	sc := &db.Scope{StoreID: 7, Role: db.RoleStaff}
	ctx := withScope(context.Background(), sc)
	got, ok := ScopeFrom(ctx)
	if !ok || got.StoreID != 7 {
		t.Fatalf("scope = %+v, ok = %v", got, ok)
	}
	if _, ok := ScopeFrom(context.Background()); ok {
		t.Fatal("empty context must not carry a scope")
	}
}
