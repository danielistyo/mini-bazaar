package orders

import (
	"context"
	"errors"
	"strings"
	"time"

	"bazaar-minggu/db"
	"bazaar-minggu/format"
	"bazaar-minggu/textnorm"
)

// GetOrder returns one order by code or id.
func (s *Service) GetOrder(ctx context.Context, eventID, ref string) (*OrderView, error) {
	event, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	id, err := s.db.GetOrderIDByRef(ctx, event.ID, ref)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Order %s tidak ditemukan di event %s.", ref, event.ID)
		}
		return nil, err
	}
	order, err := s.db.GetOrderDetail(ctx, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Order tidak ditemukan.")
		}
		return nil, err
	}
	return &OrderView{Order: order, Formatted: format.OrderDetailText(order)}, nil
}

// SearchOrders finds orders by customer, product, alias, or code.
func (s *Service) SearchOrders(ctx context.Context, eventID, keyword string) (*SearchView, error) {
	event, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(keyword) == "" {
		return nil, errf("invalid_input", "Masukkan kata kunci pencarian.")
	}
	rows, err := s.db.SearchOrders(ctx, event.ID, keyword)
	if err != nil {
		return nil, err
	}
	return &SearchView{Orders: rows, Formatted: format.SearchResults(rows)}, nil
}

// MarkOrderPaid sets or clears payment status.
func (s *Service) MarkOrderPaid(ctx context.Context, orderID int64, paid bool) (*OrderView, error) {
	order, err := s.db.SetOrderPaid(ctx, orderID, paid, time.Now().Unix())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Order tidak ditemukan.")
		}
		return nil, err
	}
	return &OrderView{Order: order, Formatted: format.PaidMessage(order, paid)}, nil
}

// GetSummary builds the event summary, optionally for one customer.
func (s *Service) GetSummary(ctx context.Context, eventID, customer string) (*SummaryView, error) {
	event, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}

	var customerID *int64
	var customerName string
	if strings.TrimSpace(customer) != "" {
		normalized := textnorm.Name(customer)
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
			customerID = &id
			customerName = exact.DisplayName
		} else if len(matches) > 0 {
			names := make([]string, 0, len(matches))
			for _, m := range matches {
				names = append(names, m.DisplayName)
			}
			return nil, errf("ambiguous_customer", "Customer mirip: %s. Pakai yang mana?", strings.Join(names, ", "))
		} else {
			return nil, errf("not_found", "Customer %s tidak ditemukan.", customer)
		}
	}

	summary, err := s.db.GetSummary(ctx, event.ID, customerID)
	if err != nil {
		return nil, err
	}
	return &SummaryView{Summary: summary, Formatted: format.Summary(summary, customerName)}, nil
}
