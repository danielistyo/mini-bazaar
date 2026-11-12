package mcpserver

import (
	"context"
	"errors"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"bazaar-minggu/orders"
)

// ToolOutput is the common result envelope.
type ToolOutput struct {
	OK        bool   `json:"ok" jsonschema:"whether the tool succeeded"`
	Code      string `json:"code,omitempty" jsonschema:"machine-readable error code"`
	Message   string `json:"message,omitempty" jsonschema:"human-readable error message"`
	Formatted string `json:"formatted,omitempty" jsonschema:"exact WhatsApp text to relay verbatim"`
	Data      any    `json:"data,omitempty" jsonschema:"structured result"`
}

type listEventsInput struct{}

type createEventInput struct {
	Name      string `json:"name" jsonschema:"event name, for example Bazar KPKM"`
	EventDate string `json:"event_date" jsonschema:"event date in YYYY-MM-DD, WIB"`
	CutoffAt  string `json:"cutoff_at,omitempty" jsonschema:"optional cutoff in RFC3339 or YYYY-MM-DD HH:MM WIB"`
	Code      string `json:"code,omitempty" jsonschema:"optional event code; default EVENT-YYYY-MM-DD"`
}

type setActiveEventInput struct {
	EventID string `json:"event_id" jsonschema:"event code"`
}

type getMenuInput struct {
	EventID string `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
}

type findProductInput struct {
	SearchTerm string `json:"search_term" jsonschema:"product name, alias, or typo"`
	EventID    string `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
}

type setProductAvailableInput struct {
	ProductID int64  `json:"product_id" jsonschema:"product id from get_menu or find_product"`
	Available bool   `json:"available" jsonschema:"false means sold out for this event; true means available again"`
	EventID   string `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
}

type upsertProductInput struct {
	EventID   string    `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
	ProductID int64     `json:"product_id,omitempty" jsonschema:"existing product id for an update; omit to add a new product"`
	Name      string    `json:"name" jsonschema:"canonical product name"`
	Category  string    `json:"category" jsonschema:"Makanan, Cemilan, or Minuman"`
	Price     int64     `json:"price" jsonschema:"integer Rupiah price for this event"`
	Aliases   *[]string `json:"aliases,omitempty" jsonschema:"optional aliases; omit on update to keep them, [] to clear"`
	Available *bool     `json:"available,omitempty" jsonschema:"optional listing state for this event; true by default when adding"`
}

type setProductEnabledInput struct {
	EventID   string `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
	ProductID int64  `json:"product_id" jsonschema:"product id"`
	Enabled   bool   `json:"enabled" jsonschema:"true to list on this event's menu, false to remove it; other events and order history stay"`
}

type findCustomerInput struct {
	Name string `json:"name" jsonschema:"customer name as spoken by the user"`
}

type createOrderDraftInput struct {
	EventID      string                  `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
	CustomerName string                  `json:"customer_name" jsonschema:"customer name; the person the food is for"`
	CustomerID   *int64                  `json:"customer_id,omitempty" jsonschema:"existing customer id chosen after find_customer"`
	NewCustomer  bool                    `json:"new_customer,omitempty" jsonschema:"true only after the user confirms this is a new customer despite similar names"`
	Items        []orders.DraftItemInput `json:"items" jsonschema:"products, quantities, and optional notes"`
	Notes        string                  `json:"notes,omitempty" jsonschema:"optional order note"`
}

type createEditDraftInput struct {
	OrderID int64                   `json:"order_id" jsonschema:"internal order id from get_order or search_orders"`
	Items   []orders.DraftItemInput `json:"items" jsonschema:"full replacement item list"`
	Notes   string                  `json:"notes,omitempty" jsonschema:"optional order note"`
}

type createCancelDraftInput struct {
	OrderID int64 `json:"order_id" jsonschema:"internal order id from get_order or search_orders"`
}

type commitDraftInput struct {
	DraftCode string `json:"draft_code" jsonschema:"4-digit code shown in the preview, for example 4821"`
}

type getOrderInput struct {
	OrderRef string `json:"order_ref" jsonschema:"order code like #BK-001 or internal id"`
	EventID  string `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
}

type searchOrdersInput struct {
	Keyword string `json:"keyword" jsonschema:"customer, product, alias, or order code"`
	EventID string `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
}

type markOrderPaidInput struct {
	OrderID int64 `json:"order_id" jsonschema:"internal order id from get_order or search_orders"`
	Paid    bool  `json:"paid" jsonschema:"true to mark paid, false to mark unpaid"`
}

type getSummaryInput struct {
	EventID  string `json:"event_id,omitempty" jsonschema:"event code; defaults to the active event"`
	Customer string `json:"customer,omitempty" jsonschema:"optional customer name filter"`
}

type handlers struct {
	svc *orders.Service
}

func ok(formatted string, data any) ToolOutput {
	return ToolOutput{OK: true, Formatted: formatted, Data: data}
}

func fail(err error) ToolOutput {
	var oe *orders.Error
	if errors.As(err, &oe) {
		return ToolOutput{OK: false, Code: oe.Code, Message: oe.Message}
	}
	slog.Error("tool failed", "err", err)
	return ToolOutput{OK: false, Code: "internal", Message: "Terjadi kesalahan internal."}
}

// New builds the MCP server with all order tools.
func New(svc *orders.Service) *mcp.Server {
	h := &handlers{svc: svc}
	s := mcp.NewServer(&mcp.Implementation{Name: "bazaar-mcp", Version: "0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_events",
		Description: "List all bazaar events with active flag, order count, and Omzet. Use before switching events.",
	}, h.listEvents)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_event",
		Description: "Create an inactive bazaar event. Use set_active_event to activate it. Never assume a date; ask the user.",
	}, h.createEvent)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_active_event",
		Description: "Activate one event. New orders default to the active event. Changing it does not move existing drafts.",
	}, h.setActiveEvent)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_menu",
		Description: "Get products with category, price, per-event availability, and aliases. Defaults to the active event.",
	}, h.getMenu)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_product",
		Description: "Resolve a product name, alias, or typo. Returns candidates. Ask the user when more than one matches; never invent a product or price.",
	}, h.findProduct)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_product_available",
		Description: "Mark a product sold out or available for one event. Reversible. Use only after the user says it is habis or available again.",
	}, h.setProductAvailable)

	mcp.AddTool(s, &mcp.Tool{
		Name: "upsert_product",
		Description: "Add or update a product on one event's menu. Only the organizer should use this, preferably in DM. " +
			"Pass product_id to update; omit it to add. Prices and listing are per event; the catalog name, category, and aliases are shared. " +
			"Show a preview and wait for OK before calling. Existing orders keep their snapshot.",
	}, h.upsertProduct)

	mcp.AddTool(s, &mcp.Tool{
		Name: "set_product_enabled",
		Description: "List or remove a product on one event's menu. Other events and order history stay. " +
			"For temporary sold-out use set_product_available instead.",
	}, h.setProductEnabled)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_customer",
		Description: "Find existing customers by name, including near matches such as Bu Susi vs Susi. Ask the user before reusing or creating a customer.",
	}, h.findCustomer)

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_order_draft",
		Description: "Prepare a new order without saving it. Resolve products with find_product and customers with find_customer first. " +
			"Show the returned preview and wait for the user to reply OK <4-digit code> before commit_draft. " +
			"Prices and totals come only from the database.",
	}, h.createOrderDraft)

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_edit_draft",
		Description: "Prepare a full replacement of an order's items. Show the before/after preview and wait for OK <4-digit code>. " +
			"Editing a paid order resets it to unpaid.",
	}, h.createEditDraft)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_cancel_draft",
		Description: "Prepare a cancellation. Show the preview and wait for OK <4-digit code>. Cancellation is soft; the order stays in history.",
	}, h.createCancelDraft)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "commit_draft",
		Description: "Apply a pending draft after the user explicitly replies OK <4-digit code>. Idempotent. Never call it on a plain yes.",
	}, h.commitDraft)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_order",
		Description: "Get one order by code (#BK-001) or internal id, including items, total, status, and payment.",
	}, h.getOrder)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_orders",
		Description: "Find orders by customer, product, alias, or order code. Returns compact rows with internal ids.",
	}, h.searchOrders)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "mark_order_paid",
		Description: "Mark an order paid or unpaid. Reversible and needs no draft. Omzet is separate from cash collected.",
	}, h.markOrderPaid)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_summary",
		Description: "Get per-customer orders, per-product quantities, Omzet, paid/unpaid totals, and cancelled count. Relay the formatted text verbatim; never recalculate.",
	}, h.getSummary)

	return s
}

func (h *handlers) listEvents(ctx context.Context, _ *mcp.CallToolRequest, _ listEventsInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.ListEvents(ctx)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) createEvent(ctx context.Context, _ *mcp.CallToolRequest, in createEventInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.CreateEvent(ctx, in.Name, in.EventDate, in.CutoffAt, in.Code)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) setActiveEvent(ctx context.Context, _ *mcp.CallToolRequest, in setActiveEventInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.SetActiveEvent(ctx, in.EventID)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) getMenu(ctx context.Context, _ *mcp.CallToolRequest, in getMenuInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.GetMenu(ctx, in.EventID)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) findProduct(ctx context.Context, _ *mcp.CallToolRequest, in findProductInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.FindProduct(ctx, in.SearchTerm, in.EventID)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) setProductAvailable(ctx context.Context, _ *mcp.CallToolRequest, in setProductAvailableInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.SetProductAvailable(ctx, in.ProductID, in.Available, in.EventID)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) upsertProduct(ctx context.Context, _ *mcp.CallToolRequest, in upsertProductInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.UpsertProduct(ctx, in.EventID, in.ProductID, in.Name, in.Category, in.Price, in.Aliases, in.Available)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) setProductEnabled(ctx context.Context, _ *mcp.CallToolRequest, in setProductEnabledInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.SetProductEnabled(ctx, in.EventID, in.ProductID, in.Enabled)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) findCustomer(ctx context.Context, _ *mcp.CallToolRequest, in findCustomerInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.FindCustomer(ctx, in.Name)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) createOrderDraft(ctx context.Context, _ *mcp.CallToolRequest, in createOrderDraftInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.CreateOrderDraft(ctx, in.EventID, in.CustomerName, in.CustomerID, in.NewCustomer, in.Items, in.Notes)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Preview, res), nil
}

func (h *handlers) createEditDraft(ctx context.Context, _ *mcp.CallToolRequest, in createEditDraftInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.CreateEditDraft(ctx, in.OrderID, in.Items, in.Notes)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Preview, res), nil
}

func (h *handlers) createCancelDraft(ctx context.Context, _ *mcp.CallToolRequest, in createCancelDraftInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.CreateCancelDraft(ctx, in.OrderID)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Preview, res), nil
}

func (h *handlers) commitDraft(ctx context.Context, _ *mcp.CallToolRequest, in commitDraftInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.CommitDraft(ctx, in.DraftCode)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) getOrder(ctx context.Context, _ *mcp.CallToolRequest, in getOrderInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.GetOrder(ctx, in.EventID, in.OrderRef)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) searchOrders(ctx context.Context, _ *mcp.CallToolRequest, in searchOrdersInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.SearchOrders(ctx, in.EventID, in.Keyword)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) markOrderPaid(ctx context.Context, _ *mcp.CallToolRequest, in markOrderPaidInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.MarkOrderPaid(ctx, in.OrderID, in.Paid)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}

func (h *handlers) getSummary(ctx context.Context, _ *mcp.CallToolRequest, in getSummaryInput) (*mcp.CallToolResult, ToolOutput, error) {
	res, err := h.svc.GetSummary(ctx, in.EventID, in.Customer)
	if err != nil {
		return nil, fail(err), nil
	}
	return nil, ok(res.Formatted, res), nil
}
