package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"bazaar-minggu/db"
	"bazaar-minggu/format"
	"bazaar-minggu/textnorm"
)

// DraftItemInput is one requested product and quantity.
type DraftItemInput struct {
	ProductID int64  `json:"product_id" jsonschema:"product id from find_product or get_menu"`
	Quantity  int    `json:"quantity" jsonschema:"positive integer"`
	Note      string `json:"note,omitempty" jsonschema:"optional item note, for example tanpa sambal"`
}

// DraftView is a pending change ready for confirmation.
type DraftView struct {
	DraftID          string `json:"draft_id,omitempty"`
	ShortCode        string `json:"short_code,omitempty"`
	Kind             string `json:"kind,omitempty"`
	EventID          string `json:"event_id,omitempty"`
	CustomerName     string `json:"customer_name,omitempty"`
	Total            int64  `json:"total,omitempty"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
	AlreadyCancelled bool   `json:"already_cancelled,omitempty"`
	Preview          string `json:"preview"`
}

func (s *Service) buildDraftItems(ctx context.Context, eventID string, inputs []DraftItemInput) ([]db.DraftItem, int64, error) {
	if len(inputs) == 0 {
		return nil, 0, errf("invalid_input", "Order belum punya item.")
	}
	items := make([]db.DraftItem, 0, len(inputs))
	var total int64
	for _, in := range inputs {
		if in.Quantity <= 0 {
			return nil, 0, errf("invalid_input", "Jumlah untuk produk %d harus lebih dari 0.", in.ProductID)
		}
		p, err := s.db.GetMenuProduct(ctx, eventID, in.ProductID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, 0, errf("not_found", "Produk %d tidak ditemukan.", in.ProductID)
			}
			return nil, 0, err
		}
		if !p.Available {
			return nil, 0, errf("sold_out", "%s sedang habis.", p.Name)
		}
		items = append(items, db.DraftItem{
			ProductID: p.ID,
			Name:      p.Name,
			Quantity:  in.Quantity,
			UnitPrice: p.Price,
			Note:      strings.TrimSpace(in.Note),
		})
		total += int64(in.Quantity) * p.Price
	}
	return items, total, nil
}

// CreateOrderDraft prepares a new order without saving it.
func (s *Service) CreateOrderDraft(ctx context.Context, eventID, customerName string, customerID *int64, newCustomer bool, inputs []DraftItemInput, notes string) (*DraftView, error) {
	event, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	customerName = strings.TrimSpace(customerName)

	var cid *int64
	var display string
	if customerID != nil {
		c, err := s.db.GetCustomer(ctx, *customerID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, errf("not_found", "Customer tidak ditemukan.")
			}
			return nil, err
		}
		id := c.ID
		cid = &id
		display = c.DisplayName
	} else {
		if customerName == "" {
			return nil, errf("invalid_input", "Atas nama siapa?")
		}
		normalized := textnorm.Name(customerName)
		if normalized == "" {
			return nil, errf("invalid_input", "Nama customer tidak valid.")
		}
		matches, err := s.db.FindCustomers(ctx, normalized)
		if err != nil {
			return nil, err
		}
		var exact *db.CustomerMatch
		for i := range matches {
			if matches[i].NormalizedName == normalized {
				exact = &matches[i]
				break
			}
		}
		if exact != nil {
			id := exact.ID
			cid = &id
			display = exact.DisplayName
		} else if len(matches) > 0 && !newCustomer {
			names := make([]string, 0, len(matches))
			for _, m := range matches {
				names = append(names, m.DisplayName)
			}
			return nil, errf("ambiguous_customer", "Customer mirip: %s. Pakai yang mana, atau ini customer baru?", strings.Join(names, ", "))
		} else {
			display = customerName
		}
	}

	if cid != nil {
		existing, err := s.db.GetConfirmedOrderForCustomer(ctx, event.ID, *cid)
		if err == nil {
			return nil, errf("existing_order", "%s sudah punya order %s. Tambahkan item ke order itu?", display, existing.Code)
		}
		if !errors.Is(err, db.ErrNotFound) {
			return nil, err
		}
	}

	items, total, err := s.buildDraftItems(ctx, event.ID, inputs)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(db.DraftPayload{Items: items, Notes: strings.TrimSpace(notes)})
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	expires := now + int64(draftTTL.Seconds())
	draft := db.Draft{
		Kind:         "create",
		EventID:      event.ID,
		CustomerID:   cid,
		CustomerName: &display,
		Payload:      string(payload),
		Total:        &total,
		Status:       "pending",
		ExpiresAt:    expires,
		CreatedAt:    now,
	}
	id, code, err := s.storeDraft(ctx, draft)
	if err != nil {
		return nil, err
	}
	return &DraftView{
		DraftID:      id,
		ShortCode:    code,
		Kind:         "create",
		EventID:      event.ID,
		CustomerName: display,
		Total:        total,
		ExpiresAt:    expires,
		Preview:      format.OrderPreview(event, display, items, total, code),
	}, nil
}

// CreateEditDraft prepares a full replacement of an order's items.
func (s *Service) CreateEditDraft(ctx context.Context, orderID int64, inputs []DraftItemInput, notes string) (*DraftView, error) {
	order, err := s.db.GetOrderDetail(ctx, orderID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Order tidak ditemukan.")
		}
		return nil, err
	}
	if order.Status != "confirmed" {
		return nil, errf("order_cancelled", "Order %s sudah dibatalkan.", order.Code)
	}
	items, total, err := s.buildDraftItems(ctx, order.EventID, inputs)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(db.DraftPayload{Items: items, Notes: strings.TrimSpace(notes)})
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	expires := now + int64(draftTTL.Seconds())
	eventID := order.EventID
	customerID := order.CustomerID
	customerName := order.CustomerName
	draft := db.Draft{
		Kind:         "edit",
		EventID:      eventID,
		OrderID:      &orderID,
		CustomerID:   &customerID,
		CustomerName: &customerName,
		Payload:      string(payload),
		Total:        &total,
		Status:       "pending",
		ExpiresAt:    expires,
		CreatedAt:    now,
	}
	id, code, err := s.storeDraft(ctx, draft)
	if err != nil {
		return nil, err
	}
	return &DraftView{
		DraftID:      id,
		ShortCode:    code,
		Kind:         "edit",
		EventID:      order.EventID,
		CustomerName: order.CustomerName,
		Total:        total,
		ExpiresAt:    expires,
		Preview:      format.EditPreview(order, items, total, code, order.IsPaid),
	}, nil
}

// CreateCancelDraft prepares a cancellation.
func (s *Service) CreateCancelDraft(ctx context.Context, orderID int64) (*DraftView, error) {
	order, err := s.db.GetOrderDetail(ctx, orderID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Order tidak ditemukan.")
		}
		return nil, err
	}
	if order.Status == "cancelled" {
		return &DraftView{
			AlreadyCancelled: true,
			CustomerName:     order.CustomerName,
			Total:            order.Total,
			Preview:          fmt.Sprintf("Order %s sudah dibatalkan.", order.Code),
		}, nil
	}

	now := time.Now().Unix()
	expires := now + int64(draftTTL.Seconds())
	eventID := order.EventID
	customerID := order.CustomerID
	customerName := order.CustomerName
	total := order.Total
	draft := db.Draft{
		Kind:         "cancel",
		EventID:      eventID,
		OrderID:      &orderID,
		CustomerID:   &customerID,
		CustomerName: &customerName,
		Payload:      "{}",
		Total:        &total,
		Status:       "pending",
		ExpiresAt:    expires,
		CreatedAt:    now,
	}
	id, code, err := s.storeDraft(ctx, draft)
	if err != nil {
		return nil, err
	}
	return &DraftView{
		DraftID:      id,
		ShortCode:    code,
		Kind:         "cancel",
		EventID:      order.EventID,
		CustomerName: order.CustomerName,
		Total:        order.Total,
		ExpiresAt:    expires,
		Preview:      format.CancelPreview(order, code),
	}, nil
}

// CommitDraft applies a pending draft.
func (s *Service) CommitDraft(ctx context.Context, draftCode string) (*OrderView, error) {
	ref := strings.TrimSpace(draftCode)
	if strings.HasPrefix(strings.ToUpper(ref), "OK ") {
		ref = strings.TrimSpace(ref[3:])
	}
	var draft *db.Draft
	var err error
	if isShortCode(ref) {
		draft, err = s.db.GetDraftByShortCode(ctx, ref)
	} else {
		draft, err = s.db.GetDraft(ctx, ref)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Draft %s tidak ditemukan.", ref)
		}
		return nil, err
	}
	order, err := s.db.CommitDraft(ctx, draft.ID, time.Now().Unix())
	if err != nil {
		return nil, s.mapDBError(err)
	}
	return &OrderView{Order: order, Formatted: format.CommitMessage(order, draft.Kind)}, nil
}

func isShortCode(ref string) bool {
	if len(ref) != 4 {
		return false
	}
	for _, r := range ref {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
