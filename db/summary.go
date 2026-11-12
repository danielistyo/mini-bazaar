package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetSummary builds the per-customer, per-product, and totals report.
func (d *DB) GetSummary(ctx context.Context, eventID string, customerID *int64) (*Summary, error) {
	event, err := d.GetEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	s := &Summary{Event: *event}

	where := `o.event_id = ? AND o.status = 'confirmed'`
	args := []any{eventID}
	if customerID != nil {
		where += ` AND o.customer_id = ?`
		args = append(args, *customerID)
	}

	orderRows, err := d.sql.QueryContext(ctx, `
		SELECT o.id, o.order_number, c.display_name, o.is_paid
		  FROM orders o
		  JOIN customers c ON c.id = o.customer_id
		 WHERE `+where+`
		 ORDER BY o.order_number`, args...)
	if err != nil {
		return nil, fmt.Errorf("summary orders: %w", err)
	}
	defer orderRows.Close()

	index := map[int64]int{}
	for orderRows.Next() {
		var o SummaryOrder
		var number int
		var paid int
		if err := orderRows.Scan(&o.OrderID, &number, &o.CustomerName, &paid); err != nil {
			return nil, fmt.Errorf("scan summary order: %w", err)
		}
		o.Code = orderCode(number)
		o.IsPaid = intToBool(paid)
		index[o.OrderID] = len(s.Orders)
		s.Orders = append(s.Orders, o)
	}
	if err := orderRows.Err(); err != nil {
		return nil, fmt.Errorf("summary order rows: %w", err)
	}

	itemRows, err := d.sql.QueryContext(ctx, `
		SELECT oi.order_id, oi.product_name, oi.quantity, oi.unit_price, oi.note
		  FROM order_items oi
		  JOIN orders o ON o.id = oi.order_id
		 WHERE `+where+`
		 ORDER BY oi.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("summary items: %w", err)
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var orderID int64
		var item SummaryItem
		var note sql.NullString
		if err := itemRows.Scan(&orderID, &item.ProductName, &item.Quantity, &item.UnitPrice, &note); err != nil {
			return nil, fmt.Errorf("scan summary item: %w", err)
		}
		item.Note = note.String
		i, ok := index[orderID]
		if !ok {
			continue
		}
		s.Orders[i].Items = append(s.Orders[i].Items, item)
		s.Orders[i].Total += int64(item.Quantity) * item.UnitPrice
	}
	if err := itemRows.Err(); err != nil {
		return nil, fmt.Errorf("summary item rows: %w", err)
	}

	for _, o := range s.Orders {
		s.OrderCount++
		s.Omzet += o.Total
		if o.IsPaid {
			s.PaidTotal += o.Total
		}
	}
	s.UnpaidTotal = s.Omzet - s.PaidTotal

	productRows, err := d.sql.QueryContext(ctx, `
		SELECT oi.product_name, SUM(oi.quantity), SUM(oi.quantity * oi.unit_price)
		  FROM order_items oi
		  JOIN orders o ON o.id = oi.order_id
		 WHERE `+where+`
		 GROUP BY oi.product_name
		 ORDER BY SUM(oi.quantity) DESC, oi.product_name`, args...)
	if err != nil {
		return nil, fmt.Errorf("summary products: %w", err)
	}
	defer productRows.Close()
	for productRows.Next() {
		var p ProductTotal
		if err := productRows.Scan(&p.Name, &p.Quantity, &p.Total); err != nil {
			return nil, fmt.Errorf("scan product total: %w", err)
		}
		s.ProductTotals = append(s.ProductTotals, p)
	}
	if err := productRows.Err(); err != nil {
		return nil, fmt.Errorf("product total rows: %w", err)
	}

	cancelWhere := `o.event_id = ? AND o.status = 'cancelled'`
	cancelArgs := []any{eventID}
	if customerID != nil {
		cancelWhere += ` AND o.customer_id = ?`
		cancelArgs = append(cancelArgs, *customerID)
	}
	err = d.sql.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_paid = 1 THEN total ELSE 0 END), 0)
		  FROM (
			SELECT o.id, o.is_paid, COALESCE(SUM(oi.quantity * oi.unit_price), 0) AS total
			  FROM orders o
			  LEFT JOIN order_items oi ON oi.order_id = o.id
			 WHERE `+cancelWhere+`
			 GROUP BY o.id
		  )`, cancelArgs...).Scan(&s.CancelledCount, &s.CancelledPaidTotal)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("summary cancelled: %w", err)
	}
	return s, nil
}
