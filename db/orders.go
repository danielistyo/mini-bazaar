package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"bazaar-minggu/textnorm"
)

// CreateDraft stores a pending draft.
func (d *DB) CreateDraft(ctx context.Context, draft Draft) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO drafts (id, short_code, kind, event_id, order_id, customer_id, customer_name, payload, total, status, expires_at, created_at, committed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		draft.ID, draft.ShortCode, draft.Kind, draft.EventID, nullableInt(draft.OrderID), nullableInt(draft.CustomerID),
		nullableStringPtr(draft.CustomerName), draft.Payload, nullableInt(draft.Total), draft.Status,
		draft.ExpiresAt, draft.CreatedAt, nullableInt(draft.CommittedAt))
	if err != nil {
		return fmt.Errorf("create draft: %w", err)
	}
	return nil
}

// GetDraft returns one draft by internal id.
func (d *DB) GetDraft(ctx context.Context, id string) (*Draft, error) {
	return getDraftWhere(ctx, d.sql, "id = ?", id)
}

// GetDraftByShortCode returns one draft by its 4-digit confirmation code.
func (d *DB) GetDraftByShortCode(ctx context.Context, code string) (*Draft, error) {
	return getDraftWhere(ctx, d.sql, "short_code = ?", code)
}

func getDraftQ(ctx context.Context, q querier, id string) (*Draft, error) {
	return getDraftWhere(ctx, q, "id = ?", id)
}

func getDraftWhere(ctx context.Context, q querier, where string, arg any) (*Draft, error) {
	var draft Draft
	var shortCode sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT id, short_code, kind, event_id, order_id, customer_id, customer_name, payload, total, status, expires_at, created_at, committed_at
		  FROM drafts WHERE `+where, arg).
		Scan(&draft.ID, &shortCode, &draft.Kind, &draft.EventID, &draft.OrderID, &draft.CustomerID, &draft.CustomerName,
			&draft.Payload, &draft.Total, &draft.Status, &draft.ExpiresAt, &draft.CreatedAt, &draft.CommittedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get draft: %w", err)
	}
	draft.ShortCode = shortCode.String
	return &draft, nil
}

// CommitDraft applies a pending draft atomically and is idempotent.
func (d *DB) CommitDraft(ctx context.Context, draftID string, now int64) (*OrderDetail, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin commit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	draft, err := getDraftQ(ctx, tx, draftID)
	if err != nil {
		return nil, err
	}
	if draft.Status == "committed" {
		if draft.OrderID == nil {
			return nil, fmt.Errorf("draft %s committed without an order", draftID)
		}
		return getOrderDetailQ(ctx, tx, *draft.OrderID)
	}
	if draft.Status == "expired" {
		return nil, ErrDraftExpired
	}
	if draft.ExpiresAt < now {
		if _, err := tx.ExecContext(ctx, `UPDATE drafts SET status = 'expired' WHERE id = ? AND status = 'pending'`, draftID); err != nil {
			return nil, fmt.Errorf("expire draft: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit expiry: %w", err)
		}
		return nil, ErrDraftExpired
	}

	var orderID int64
	switch draft.Kind {
	case "create":
		orderID, err = commitCreate(ctx, tx, draft, now)
	case "edit":
		orderID, err = commitEdit(ctx, tx, draft, now)
	case "cancel":
		orderID, err = commitCancel(ctx, tx, draft, now)
	default:
		return nil, fmt.Errorf("unknown draft kind %q", draft.Kind)
	}
	if err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE drafts SET status = 'committed', order_id = ?, committed_at = ? WHERE id = ? AND status = 'pending'`,
		orderID, now, draftID)
	if err != nil {
		return nil, fmt.Errorf("mark draft committed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("draft rows affected: %w", err)
	}
	if n != 1 {
		return nil, ErrDraftNotPending
	}

	order, err := getOrderDetailQ(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit draft: %w", err)
	}
	return order, nil
}

func commitCreate(ctx context.Context, tx *sql.Tx, draft *Draft, now int64) (int64, error) {
	var payload DraftPayload
	if err := json.Unmarshal([]byte(draft.Payload), &payload); err != nil {
		return 0, fmt.Errorf("decode draft payload: %w", err)
	}
	if len(payload.Items) == 0 {
		return 0, fmt.Errorf("draft has no items")
	}

	var customer *Customer
	var err error
	if draft.CustomerID != nil {
		customer, err = getCustomerByID(ctx, tx, *draft.CustomerID)
	} else {
		if draft.CustomerName == nil || strings.TrimSpace(*draft.CustomerName) == "" {
			return 0, fmt.Errorf("draft has no customer")
		}
		normalized := textnorm.Name(*draft.CustomerName)
		if normalized == "" {
			return 0, fmt.Errorf("customer name normalizes to empty")
		}
		customer, err = getCustomerByNormalized(ctx, tx, normalized)
		if errors.Is(err, ErrNotFound) {
			customer, err = createCustomer(ctx, tx, strings.TrimSpace(*draft.CustomerName), normalized, now)
		}
	}
	if err != nil {
		return 0, err
	}

	existing, err := confirmedOrderForCustomerQ(ctx, tx, draft.EventID, customer.ID)
	if err == nil {
		return 0, &ExistingOrderError{OrderID: existing.OrderID, Code: existing.Code}
	}
	if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	for _, item := range payload.Items {
		if err := validateDraftItem(ctx, tx, draft.EventID, item); err != nil {
			return 0, err
		}
	}

	number, err := nextOrderNumberQ(ctx, tx, draft.EventID)
	if err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO orders (event_id, customer_id, order_number, status, is_paid, notes, created_at, updated_at)
		VALUES (?, ?, ?, 'confirmed', 0, ?, ?, ?)`,
		draft.EventID, customer.ID, number, nullableString(payload.Notes), now, now)
	if err != nil {
		return 0, fmt.Errorf("insert order: %w", err)
	}
	orderID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("order id: %w", err)
	}

	var total int64
	for _, item := range payload.Items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO order_items (order_id, product_id, product_name, quantity, unit_price, note)
			VALUES (?, ?, ?, ?, ?, ?)`,
			orderID, item.ProductID, item.Name, item.Quantity, item.UnitPrice, nullableString(item.Note)); err != nil {
			return 0, fmt.Errorf("insert order item: %w", err)
		}
		total += int64(item.Quantity) * item.UnitPrice
	}

	if err := audit(ctx, tx, orderID, "created", map[string]any{"total": total, "items": len(payload.Items)}, now); err != nil {
		return 0, err
	}
	return orderID, nil
}

func commitEdit(ctx context.Context, tx *sql.Tx, draft *Draft, now int64) (int64, error) {
	if draft.OrderID == nil {
		return 0, fmt.Errorf("edit draft has no order")
	}
	var eventID string
	var status string
	var isPaid int
	err := tx.QueryRowContext(ctx,
		`SELECT event_id, status, is_paid FROM orders WHERE id = ?`, *draft.OrderID).
		Scan(&eventID, &status, &isPaid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("get order for edit: %w", err)
	}
	if status != "confirmed" {
		return 0, ErrOrderNotConfirmed
	}

	var payload DraftPayload
	if err := json.Unmarshal([]byte(draft.Payload), &payload); err != nil {
		return 0, fmt.Errorf("decode edit payload: %w", err)
	}
	if len(payload.Items) == 0 {
		return 0, fmt.Errorf("edit draft has no items")
	}
	for _, item := range payload.Items {
		if err := validateDraftItem(ctx, tx, eventID, item); err != nil {
			return 0, err
		}
	}

	var before int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(quantity * unit_price), 0) FROM order_items WHERE order_id = ?`, *draft.OrderID).
		Scan(&before); err != nil {
		return 0, fmt.Errorf("edit before total: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM order_items WHERE order_id = ?`, *draft.OrderID); err != nil {
		return 0, fmt.Errorf("clear order items: %w", err)
	}

	var after int64
	for _, item := range payload.Items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO order_items (order_id, product_id, product_name, quantity, unit_price, note)
			VALUES (?, ?, ?, ?, ?, ?)`,
			*draft.OrderID, item.ProductID, item.Name, item.Quantity, item.UnitPrice, nullableString(item.Note)); err != nil {
			return 0, fmt.Errorf("insert edited item: %w", err)
		}
		after += int64(item.Quantity) * item.UnitPrice
	}

	if intToBool(isPaid) {
		if _, err := tx.ExecContext(ctx,
			`UPDATE orders SET is_paid = 0, paid_at = NULL, notes = ?, updated_at = ? WHERE id = ?`,
			nullableString(payload.Notes), now, *draft.OrderID); err != nil {
			return 0, fmt.Errorf("reset payment: %w", err)
		}
		if err := audit(ctx, tx, *draft.OrderID, "payment_reset", map[string]any{"reason": "order edited"}, now); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE orders SET notes = ?, updated_at = ? WHERE id = ?`,
			nullableString(payload.Notes), now, *draft.OrderID); err != nil {
			return 0, fmt.Errorf("update order: %w", err)
		}
	}

	if err := audit(ctx, tx, *draft.OrderID, "edited", map[string]any{"before_total": before, "after_total": after}, now); err != nil {
		return 0, err
	}
	return *draft.OrderID, nil
}

func commitCancel(ctx context.Context, tx *sql.Tx, draft *Draft, now int64) (int64, error) {
	if draft.OrderID == nil {
		return 0, fmt.Errorf("cancel draft has no order")
	}
	var status string
	var isPaid int
	err := tx.QueryRowContext(ctx,
		`SELECT status, is_paid FROM orders WHERE id = ?`, *draft.OrderID).Scan(&status, &isPaid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("get order for cancel: %w", err)
	}
	if status == "cancelled" {
		return *draft.OrderID, nil
	}

	var total int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(quantity * unit_price), 0) FROM order_items WHERE order_id = ?`, *draft.OrderID).
		Scan(&total); err != nil {
		return 0, fmt.Errorf("cancel total: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE orders SET status = 'cancelled', cancelled_at = ?, updated_at = ? WHERE id = ?`,
		now, now, *draft.OrderID); err != nil {
		return 0, fmt.Errorf("cancel order: %w", err)
	}
	if err := audit(ctx, tx, *draft.OrderID, "cancelled", map[string]any{"was_paid": intToBool(isPaid), "total": total}, now); err != nil {
		return 0, err
	}
	return *draft.OrderID, nil
}

// SetOrderPaid marks an order paid or unpaid.
func (d *DB) SetOrderPaid(ctx context.Context, orderID int64, paid bool, now int64) (*OrderDetail, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin paid: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, orderID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get order: %w", err)
	}

	action := "marked_unpaid"
	if paid {
		action = "marked_paid"
		if _, err := tx.ExecContext(ctx,
			`UPDATE orders SET is_paid = 1, paid_at = ?, updated_at = ? WHERE id = ?`,
			now, now, orderID); err != nil {
			return nil, fmt.Errorf("mark paid: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE orders SET is_paid = 0, paid_at = NULL, updated_at = ? WHERE id = ?`,
			now, orderID); err != nil {
			return nil, fmt.Errorf("mark unpaid: %w", err)
		}
	}
	if err := audit(ctx, tx, orderID, action, map[string]any{"status": status}, now); err != nil {
		return nil, err
	}

	order, err := getOrderDetailQ(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit paid: %w", err)
	}
	return order, nil
}

// GetOrderDetail returns a full order.
func (d *DB) GetOrderDetail(ctx context.Context, orderID int64) (*OrderDetail, error) {
	return getOrderDetailQ(ctx, d.sql, orderID)
}

// GetOrderIDByRef resolves "#BK-001", "BK-001", or an internal id within an event.
func (d *DB) GetOrderIDByRef(ctx context.Context, eventID, ref string) (int64, error) {
	ref = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "#"))
	if ref == "" {
		return 0, ErrNotFound
	}
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		var found int64
		err := d.sql.QueryRowContext(ctx, `SELECT id FROM orders WHERE id = ? AND event_id = ?`, id, eventID).Scan(&found)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, ErrNotFound
			}
			return 0, fmt.Errorf("get order by id: %w", err)
		}
		return found, nil
	}
	upper := strings.ToUpper(ref)
	if !strings.HasPrefix(upper, "BK-") {
		return 0, ErrNotFound
	}
	number, err := strconv.Atoi(strings.TrimPrefix(upper, "BK-"))
	if err != nil {
		return 0, ErrNotFound
	}
	var found int64
	err = d.sql.QueryRowContext(ctx, `SELECT id FROM orders WHERE event_id = ? AND order_number = ?`, eventID, number).Scan(&found)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("get order by number: %w", err)
	}
	return found, nil
}

// GetConfirmedOrderForCustomer returns the customer's confirmed order in an event.
func (d *DB) GetConfirmedOrderForCustomer(ctx context.Context, eventID string, customerID int64) (*ExistingOrderError, error) {
	return confirmedOrderForCustomerQ(ctx, d.sql, eventID, customerID)
}

func confirmedOrderForCustomerQ(ctx context.Context, q querier, eventID string, customerID int64) (*ExistingOrderError, error) {
	var id int64
	var number int
	err := q.QueryRowContext(ctx,
		`SELECT id, order_number FROM orders WHERE event_id = ? AND customer_id = ? AND status = 'confirmed' LIMIT 1`,
		eventID, customerID).Scan(&id, &number)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("confirmed order lookup: %w", err)
	}
	return &ExistingOrderError{OrderID: id, Code: orderCode(number)}, nil
}

func nextOrderNumberQ(ctx context.Context, q querier, eventID string) (int, error) {
	var number int
	if err := q.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(order_number), 0) + 1 FROM orders WHERE event_id = ?`, eventID).Scan(&number); err != nil {
		return 0, fmt.Errorf("next order number: %w", err)
	}
	return number, nil
}

func validateDraftItem(ctx context.Context, q querier, eventID string, item DraftItem) error {
	if item.Quantity <= 0 {
		return fmt.Errorf("invalid quantity for %q", item.Name)
	}
	if item.UnitPrice < 0 {
		return fmt.Errorf("invalid price for %q", item.Name)
	}
	name, _, available, err := productAvailabilityQ(ctx, q, eventID, item.ProductID)
	if err != nil {
		return err
	}
	if !available {
		return &SoldOutError{ProductName: name}
	}
	return nil
}

func productAvailabilityQ(ctx context.Context, q querier, eventID string, productID int64) (string, int64, bool, error) {
	var name string
	var price int64
	var available int
	err := q.QueryRowContext(ctx, `
		SELECT p.name, p.price,
		       CASE WHEN p.is_available = 1
		                 AND NOT EXISTS (SELECT 1 FROM event_sold_out es
		                                  WHERE es.event_id = ? AND es.product_id = p.id)
		            THEN 1 ELSE 0 END
		  FROM products p WHERE p.id = ?`, eventID, productID).Scan(&name, &price, &available)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, false, ErrNotFound
		}
		return "", 0, false, fmt.Errorf("product availability: %w", err)
	}
	return name, price, intToBool(available), nil
}

func getOrderDetailQ(ctx context.Context, q querier, orderID int64) (*OrderDetail, error) {
	var o OrderDetail
	var number int
	var isPaid int
	var notes sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT o.id, o.event_id, e.name, e.event_date, o.customer_id, c.display_name,
		       o.order_number, o.status, o.is_paid, o.paid_at, o.notes, o.created_at, o.updated_at, o.cancelled_at
		  FROM orders o
		  JOIN customers c ON c.id = o.customer_id
		  JOIN bazaar_events e ON e.id = o.event_id
		 WHERE o.id = ?`, orderID).
		Scan(&o.ID, &o.EventID, &o.EventName, &o.EventDate, &o.CustomerID, &o.CustomerName,
			&number, &o.Status, &isPaid, &o.PaidAt, &notes, &o.CreatedAt, &o.UpdatedAt, &o.CancelledAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get order detail: %w", err)
	}
	o.Code = orderCode(number)
	o.IsPaid = intToBool(isPaid)
	o.Notes = notes.String

	rows, err := q.QueryContext(ctx, `
		SELECT product_id, product_name, quantity, unit_price, note
		  FROM order_items WHERE order_id = ? ORDER BY id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("get order items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item OrderItemDetail
		var note sql.NullString
		if err := rows.Scan(&item.ProductID, &item.ProductName, &item.Quantity, &item.UnitPrice, &note); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		item.Note = note.String
		o.Items = append(o.Items, item)
		o.Total += int64(item.Quantity) * item.UnitPrice
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order item rows: %w", err)
	}
	return &o, nil
}

// SearchOrders finds orders by customer, product, alias, or order code.
func (d *DB) SearchOrders(ctx context.Context, eventID, keyword string) ([]OrderRow, error) {
	nameTerm := textnorm.Name(keyword)
	prodTerm := textnorm.Term(keyword)
	rawTerm := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(keyword), "#")))
	if nameTerm == "" {
		nameTerm = prodTerm
	}
	if prodTerm == "" {
		return nil, nil
	}

	rows, err := d.sql.QueryContext(ctx, `
		SELECT DISTINCT o.id, o.order_number, c.display_name, o.status, o.is_paid,
		       COALESCE((SELECT SUM(oi.quantity * oi.unit_price)
		                   FROM order_items oi WHERE oi.order_id = o.id), 0) AS total
		  FROM orders o
		  JOIN customers c ON c.id = o.customer_id
		  LEFT JOIN order_items oi ON oi.order_id = o.id
		  LEFT JOIN products p ON p.id = oi.product_id
		  LEFT JOIN product_aliases a ON a.product_id = p.id
		 WHERE o.event_id = ?
		   AND (c.normalized_name LIKE '%' || ? || '%'
		        OR p.normalized_name LIKE '%' || ? || '%'
		        OR a.normalized_alias LIKE '%' || ? || '%'
		        OR ('BK-' || printf('%03d', o.order_number)) LIKE '%' || ? || '%')
		 ORDER BY o.order_number
		 LIMIT 20`,
		eventID, nameTerm, prodTerm, prodTerm, rawTerm)
	if err != nil {
		return nil, fmt.Errorf("search orders: %w", err)
	}
	defer rows.Close()

	var out []OrderRow
	for rows.Next() {
		var row OrderRow
		var number int
		var isPaid int
		if err := rows.Scan(&row.ID, &number, &row.CustomerName, &row.Status, &isPaid, &row.Total); err != nil {
			return nil, fmt.Errorf("scan search order: %w", err)
		}
		row.Code = orderCode(number)
		row.IsPaid = intToBool(isPaid)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search rows: %w", err)
	}
	return out, nil
}

func audit(ctx context.Context, q querier, orderID int64, action string, details any, now int64) error {
	b, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode audit: %w", err)
	}
	if _, err := q.ExecContext(ctx,
		`INSERT INTO order_audit_logs (order_id, action, details, created_at) VALUES (?, ?, ?, ?)`,
		orderID, action, string(b), now); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}

func orderCode(number int) string {
	return fmt.Sprintf("#BK-%03d", number)
}

func nullableStringPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
