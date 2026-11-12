package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// FindCustomers returns exact and near matches by normalized name.
func (d *DB) FindCustomers(ctx context.Context, normalized string) ([]CustomerMatch, error) {
	if normalized == "" {
		return nil, nil
	}
	rows, err := d.sql.QueryContext(ctx, `
		SELECT c.id, c.display_name, c.normalized_name, c.created_at,
		       (SELECT COUNT(*) FROM orders o
		         WHERE o.customer_id = c.id AND o.status = 'confirmed') AS order_count
		  FROM customers c
		 WHERE c.normalized_name = ? OR c.normalized_name LIKE '%' || ? || '%'
		 ORDER BY CASE WHEN c.normalized_name = ? THEN 0 ELSE 1 END, c.display_name
		 LIMIT 10`, normalized, normalized, normalized)
	if err != nil {
		return nil, fmt.Errorf("find customers: %w", err)
	}
	defer rows.Close()

	var out []CustomerMatch
	for rows.Next() {
		var c CustomerMatch
		if err := rows.Scan(&c.ID, &c.DisplayName, &c.NormalizedName, &c.CreatedAt, &c.OrderCount); err != nil {
			return nil, fmt.Errorf("scan customer: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("customer rows: %w", err)
	}
	return out, nil
}

// GetCustomer returns one customer by id.
func (d *DB) GetCustomer(ctx context.Context, id int64) (*Customer, error) {
	return getCustomerByID(ctx, d.sql, id)
}

func getCustomerByID(ctx context.Context, q querier, id int64) (*Customer, error) {
	var c Customer
	err := q.QueryRowContext(ctx,
		`SELECT id, display_name, normalized_name, created_at FROM customers WHERE id = ?`, id).
		Scan(&c.ID, &c.DisplayName, &c.NormalizedName, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get customer: %w", err)
	}
	return &c, nil
}

func getCustomerByNormalized(ctx context.Context, q querier, normalized string) (*Customer, error) {
	var c Customer
	err := q.QueryRowContext(ctx,
		`SELECT id, display_name, normalized_name, created_at FROM customers WHERE normalized_name = ?`, normalized).
		Scan(&c.ID, &c.DisplayName, &c.NormalizedName, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get customer by name: %w", err)
	}
	return &c, nil
}

func createCustomer(ctx context.Context, q querier, displayName, normalized string, now int64) (*Customer, error) {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO customers (display_name, normalized_name, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(normalized_name) DO NOTHING`, displayName, normalized, now); err != nil {
		return nil, fmt.Errorf("create customer: %w", err)
	}
	return getCustomerByNormalized(ctx, q, normalized)
}
