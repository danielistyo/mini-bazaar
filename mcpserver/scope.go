package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"bazaar-minggu/db"
	"bazaar-minggu/orders"
)

// contextMetaKey is the MCP request _meta field where the runtime puts the
// caller's channel, chat and sender. The model never sees it and cannot set it.
const contextMetaKey = "id.bazaar/context"

var (
	errNoStore = &orders.Error{Code: "no_store", Message: "Chat ini belum terhubung ke store."}
	errRole    = &orders.Error{Code: "role_denied", Message: "Role ini tidak bisa memakai tool tersebut."}
)

// callContext is what the runtime injects, one per tool call.
type callContext struct {
	Channel   string `json:"channel"`
	ChatRef   string `json:"chat_ref"`
	SenderRef string `json:"sender_ref"`
}

type scopeKey struct{}

func withScope(ctx context.Context, sc *db.Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, sc)
}

// ScopeFrom returns the store and role this tool call acts under.
func ScopeFrom(ctx context.Context) (*db.Scope, bool) {
	sc, ok := ctx.Value(scopeKey{}).(*db.Scope)
	return sc, ok
}

func readCallContext(req *mcp.CallToolRequest) callContext {
	if req == nil || req.Params == nil || req.Params.Meta == nil {
		return callContext{}
	}
	raw, ok := req.Params.Meta[contextMetaKey]
	if !ok {
		return callContext{}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return callContext{}
	}
	var cc callContext
	if err := json.Unmarshal(b, &cc); err != nil {
		return callContext{}
	}
	return cc
}

// resolve maps the injected context to a store and role. Missing context is a
// refusal unless the operator started the server with a pilot store.
func (h *handlers) resolve(ctx context.Context, req *mcp.CallToolRequest) (*db.Scope, error) {
	cc := readCallContext(req)
	if cc.Channel == "" || (cc.ChatRef == "" && cc.SenderRef == "") {
		if h.pilot != nil {
			return h.pilot, nil
		}
		return nil, errNoStore
	}
	sc, err := h.db.ResolveScope(ctx, cc.Channel, cc.ChatRef, cc.SenderRef)
	switch {
	case errors.Is(err, db.ErrNoStore):
		return nil, errNoStore
	case errors.Is(err, db.ErrStoreSuspended):
		return nil, &orders.Error{Code: "store_suspended", Message: "Store sedang tidak aktif."}
	case err != nil:
		return nil, err
	}
	return sc, nil
}

// scoped returns a copy of the handlers whose service is bounded to the scope.
func (h *handlers) scoped(sc *db.Scope) *handlers {
	c := *h
	c.scope = sc
	c.svc = h.svc.ForStore(sc.StoreID)
	return &c
}

// toolRoles is the minimum role per tool. A tool missing here is denied.
var toolRoles = map[string]db.Role{
	"list_events":           db.RoleStaff,
	"create_event":          db.RoleOrganizer,
	"set_active_event":      db.RoleOrganizer,
	"get_menu":              db.RoleStaff,
	"find_product":          db.RoleStaff,
	"set_product_available": db.RoleOrganizer,
	"upsert_product":        db.RoleOrganizer,
	"set_product_enabled":   db.RoleOrganizer,
	"find_customer":         db.RoleStaff,
	"create_order_draft":    db.RoleStaff,
	"create_edit_draft":     db.RoleStaff,
	"create_cancel_draft":   db.RoleStaff,
	"commit_draft":          db.RoleStaff,
	"get_order":             db.RoleStaff,
	"search_orders":         db.RoleStaff,
	"mark_order_paid":       db.RoleStaff,
	"get_summary":           db.RoleStaff,
	"create_group_claim":    db.RoleOrganizer,
}

type toolFunc[In any] func(*handlers, context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, ToolOutput, error)

// scopedTool registers a tool that needs a resolved store and a role.
func scopedTool[In any](s *mcp.Server, tool *mcp.Tool, h *handlers, fn toolFunc[In]) {
	mcp.AddTool(s, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, ToolOutput, error) {
		required, ok := toolRoles[tool.Name]
		if !ok {
			return nil, fail(errRole), nil
		}
		sc, err := h.resolve(ctx, req)
		if err != nil {
			return nil, fail(err), nil
		}
		if !sc.Role.Allows(required) {
			return nil, fail(errRole), nil
		}
		return fn(h.scoped(sc), withScope(ctx, sc), req, in)
	})
}

// openTool registers a tool that must work before a store is resolved: creating
// a store, and redeeming a claim in a chat that has no store yet.
func openTool[In any](s *mcp.Server, tool *mcp.Tool, h *handlers, fn toolFunc[In]) {
	mcp.AddTool(s, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, ToolOutput, error) {
		return fn(h, ctx, req, in)
	})
}
