package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"bazaar-minggu/textnorm"
)

// ProductInput is an add-or-update request for one event's menu.
type ProductInput struct {
	EventID   string
	ProductID int64
	Name      string
	Category  string
	Price     int64
	Aliases   *[]string
	Available *bool
}

// ProductConflictError means another product already owns the name.
type ProductConflictError struct {
	ProductID int64
	Name      string
}

func (e *ProductConflictError) Error() string {
	return fmt.Sprintf("product %q already exists (id %d)", e.Name, e.ProductID)
}

// UpsertProduct adds a catalog product and lists it on one event, or updates both.
func (d *DB) UpsertProduct(ctx context.Context, in ProductInput) (*Product, error) {
	eventID := strings.TrimSpace(in.EventID)
	name := strings.TrimSpace(in.Name)
	category := strings.TrimSpace(in.Category)
	if eventID == "" {
		return nil, fmt.Errorf("event id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("product name is required")
	}
	if category == "" {
		return nil, fmt.Errorf("product category is required")
	}
	if in.Price < 0 {
		return nil, fmt.Errorf("product price must be >= 0")
	}
	normalized := textnorm.Term(name)
	if normalized == "" {
		return nil, fmt.Errorf("product name normalizes to empty")
	}
	now := time.Now().Unix()

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin product upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var productID int64
	if in.ProductID == 0 {
		var conflictID int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM products WHERE normalized_name = ? OR name = ?`,
			normalized, name).Scan(&conflictID)
		if err == nil {
			return nil, &ProductConflictError{ProductID: conflictID, Name: name}
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("check product name: %w", err)
		}

		res, err := tx.ExecContext(ctx, `
			INSERT INTO products (name, normalized_name, category, price, is_available, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`,
			name, normalized, category, in.Price, now, now)
		if err != nil {
			return nil, fmt.Errorf("insert product: %w", err)
		}
		productID, err = res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("product id: %w", err)
		}
	} else {
		if _, err := getProductQ(ctx, tx, in.ProductID); err != nil {
			return nil, err
		}
		var conflictID int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM products WHERE (normalized_name = ? OR name = ?) AND id <> ?`,
			normalized, name, in.ProductID).Scan(&conflictID)
		if err == nil {
			return nil, &ProductConflictError{ProductID: conflictID, Name: name}
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("check product name: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE products
			   SET name = ?, normalized_name = ?, category = ?, price = ?, updated_at = ?
			 WHERE id = ?`,
			name, normalized, category, in.Price, now, in.ProductID); err != nil {
			return nil, fmt.Errorf("update product: %w", err)
		}
		productID = in.ProductID
	}

	if in.Aliases != nil {
		if err := replaceAliases(ctx, tx, productID, *in.Aliases); err != nil {
			return nil, err
		}
	}

	available := true
	var current int
	err = tx.QueryRowContext(ctx,
		`SELECT is_available FROM event_products WHERE event_id = ? AND product_id = ?`,
		eventID, productID).Scan(&current)
	switch {
	case err == nil:
		if in.Available != nil {
			available = *in.Available
		} else {
			available = intToBool(current)
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, fmt.Errorf("read event product: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_products (event_id, product_id, price, is_available, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id, product_id) DO UPDATE SET
			price = excluded.price,
			is_available = excluded.is_available,
			updated_at = excluded.updated_at`,
		eventID, productID, in.Price, boolToInt(available), now, now); err != nil {
		return nil, fmt.Errorf("upsert event product: %w", err)
	}

	product, err := getEventProductQ(ctx, tx, eventID, productID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit product upsert: %w", err)
	}
	return product, nil
}

// SetProductEnabled toggles a product's listing on one event's menu.
// Enabling a catalog product that is not listed yet adds it at the catalog price.
func (d *DB) SetProductEnabled(ctx context.Context, eventID string, productID int64, enabled bool) (*Product, error) {
	now := time.Now().Unix()
	if enabled {
		var price int64
		if err := d.sql.QueryRowContext(ctx,
			`SELECT price FROM products WHERE id = ?`, productID).Scan(&price); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("get product price: %w", err)
		}
		if _, err := d.sql.ExecContext(ctx, `
			INSERT INTO event_products (event_id, product_id, price, is_available, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?)
			ON CONFLICT(event_id, product_id) DO UPDATE SET
				is_available = 1,
				updated_at = excluded.updated_at`,
			eventID, productID, price, now, now); err != nil {
			return nil, fmt.Errorf("enable event product: %w", err)
		}
	} else {
		res, err := d.sql.ExecContext(ctx,
			`UPDATE event_products SET is_available = 0, updated_at = ?
			  WHERE event_id = ? AND product_id = ?`, now, eventID, productID)
		if err != nil {
			return nil, fmt.Errorf("disable event product: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("product rows affected: %w", err)
		}
		if n == 0 {
			return nil, ErrNotFound
		}
	}
	return getEventProductQ(ctx, d.sql, eventID, productID)
}

func getProductQ(ctx context.Context, q querier, productID int64) (*Product, error) {
	var p Product
	var available int
	err := q.QueryRowContext(ctx, `
		SELECT id, name, category, price, is_available, created_at, updated_at
		  FROM products WHERE id = ?`, productID).
		Scan(&p.ID, &p.Name, &p.Category, &p.Price, &available, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get product: %w", err)
	}
	p.IsAvailable = intToBool(available)
	return &p, nil
}

// getEventProductQ returns a product priced and flagged for one event.
func getEventProductQ(ctx context.Context, q querier, eventID string, productID int64) (*Product, error) {
	var p Product
	var available int
	err := q.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.category, ep.price, ep.is_available, p.created_at, p.updated_at
		  FROM event_products ep
		  JOIN products p ON p.id = ep.product_id
		 WHERE ep.event_id = ? AND p.id = ?`, eventID, productID).
		Scan(&p.ID, &p.Name, &p.Category, &p.Price, &available, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get event product: %w", err)
	}
	p.IsAvailable = intToBool(available)
	return &p, nil
}

func replaceAliases(ctx context.Context, q querier, productID int64, aliases []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM product_aliases WHERE product_id = ?`, productID); err != nil {
		return fmt.Errorf("clear aliases: %w", err)
	}
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		normalized := textnorm.Term(alias)
		if alias == "" || normalized == "" {
			continue
		}
		if _, err := q.ExecContext(ctx,
			`INSERT OR IGNORE INTO product_aliases (product_id, alias, normalized_alias) VALUES (?, ?, ?)`,
			productID, alias, normalized); err != nil {
			return fmt.Errorf("insert alias %q: %w", alias, err)
		}
	}
	return nil
}
