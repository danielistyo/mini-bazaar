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

// SeedProducts upserts menu rows for one event and replaces each product's aliases.
func (d *DB) SeedProducts(ctx context.Context, eventID string, rows []MenuRow) (int, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Unix()
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			return 0, fmt.Errorf("empty product name")
		}
		normalized := textnorm.Term(name)
		if normalized == "" {
			return 0, fmt.Errorf("product name normalizes to empty: %q", name)
		}
		if row.Price < 0 {
			return 0, fmt.Errorf("negative price for %q", name)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO products (name, normalized_name, category, price, is_available, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(name) DO UPDATE SET
				normalized_name = excluded.normalized_name,
				category = excluded.category,
				price = excluded.price,
				updated_at = excluded.updated_at`,
			name, normalized, row.Category, row.Price, now, now); err != nil {
			return 0, fmt.Errorf("upsert product %q: %w", name, err)
		}

		var productID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM products WHERE name = ?`, name).Scan(&productID); err != nil {
			return 0, fmt.Errorf("get product id %q: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO event_products (event_id, product_id, price, is_available, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?)
			ON CONFLICT(event_id, product_id) DO UPDATE SET
				price = excluded.price,
				is_available = 1,
				updated_at = excluded.updated_at`,
			eventID, productID, row.Price, now, now); err != nil {
			return 0, fmt.Errorf("upsert event product %q: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM product_aliases WHERE product_id = ?`, productID); err != nil {
			return 0, fmt.Errorf("clear aliases %q: %w", name, err)
		}
		for _, alias := range row.Aliases {
			alias = strings.TrimSpace(alias)
			na := textnorm.Term(alias)
			if alias == "" || na == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO product_aliases (product_id, alias, normalized_alias) VALUES (?, ?, ?)`,
				productID, alias, na); err != nil {
				return 0, fmt.Errorf("insert alias %q for %q: %w", alias, name, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit seed: %w", err)
	}
	return len(rows), nil
}

// ListMenu returns the event's listed products with per-event availability and aliases.
func (d *DB) ListMenu(ctx context.Context, eventID string) ([]MenuProduct, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT p.id, p.name, p.category, ep.price, ep.is_available, p.created_at, p.updated_at,
		       CASE WHEN p.is_available = 1 AND ep.is_available = 1
		                 AND NOT EXISTS (SELECT 1 FROM event_sold_out es
		                                  WHERE es.event_id = ? AND es.product_id = p.id)
		            THEN 1 ELSE 0 END AS available
		  FROM event_products ep
		  JOIN products p ON p.id = ep.product_id
		 WHERE ep.event_id = ? AND ep.is_available = 1 AND p.is_available = 1
		 ORDER BY CASE p.category
		            WHEN 'Makanan' THEN 0
		            WHEN 'Cemilan' THEN 1
		            WHEN 'Minuman' THEN 2
		            ELSE 3 END,
		          p.name`, eventID, eventID)
	if err != nil {
		return nil, fmt.Errorf("list menu: %w", err)
	}
	defer rows.Close()

	var out []MenuProduct
	for rows.Next() {
		var p MenuProduct
		var available int
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Price, &p.IsAvailable, &p.CreatedAt, &p.UpdatedAt, &available); err != nil {
			return nil, fmt.Errorf("scan menu: %w", err)
		}
		p.Available = intToBool(available)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("menu rows: %w", err)
	}

	aliases, err := d.aliases(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Aliases = aliases[out[i].ID]
	}
	return out, nil
}

func (d *DB) aliases(ctx context.Context) (map[int64][]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT product_id, alias FROM product_aliases ORDER BY alias`)
	if err != nil {
		return nil, fmt.Errorf("list aliases: %w", err)
	}
	defer rows.Close()

	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var alias string
		if err := rows.Scan(&id, &alias); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		out[id] = append(out[id], alias)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alias rows: %w", err)
	}
	return out, nil
}

// FindProducts returns exact matches first, then partial matches.
func (d *DB) FindProducts(ctx context.Context, term, eventID string) ([]ProductMatch, error) {
	normalized := textnorm.Term(term)
	if normalized == "" {
		return nil, nil
	}

	exact, err := d.findProducts(ctx, eventID,
		`(p.normalized_name = ? OR EXISTS (SELECT 1 FROM product_aliases a
		                                   WHERE a.product_id = p.id AND a.normalized_alias = ?))`,
		normalized, normalized)
	if err != nil {
		return nil, err
	}
	if len(exact) > 0 {
		return exact, nil
	}

	return d.findProducts(ctx, eventID,
		`(p.normalized_name LIKE '%' || ? || '%' OR EXISTS (SELECT 1 FROM product_aliases a
		                                                    WHERE a.product_id = p.id
		                                                      AND a.normalized_alias LIKE '%' || ? || '%'))`,
		normalized, normalized)
}

func (d *DB) findProducts(ctx context.Context, eventID, condition string, args ...any) ([]ProductMatch, error) {
	query := `
		SELECT p.id, p.name, p.category, ep.price, ep.is_available, p.created_at, p.updated_at,
		       CASE WHEN p.is_available = 1 AND ep.is_available = 1
		                 AND NOT EXISTS (SELECT 1 FROM event_sold_out es
		                                  WHERE es.event_id = ? AND es.product_id = p.id)
		            THEN 1 ELSE 0 END AS available
		  FROM event_products ep
		  JOIN products p ON p.id = ep.product_id
		 WHERE ep.event_id = ? AND ` + condition + `
		 ORDER BY p.name
		 LIMIT 10`
	fullArgs := append([]any{eventID, eventID}, args...)
	rows, err := d.sql.QueryContext(ctx, query, fullArgs...)
	if err != nil {
		return nil, fmt.Errorf("find products: %w", err)
	}
	defer rows.Close()

	var out []ProductMatch
	for rows.Next() {
		var p ProductMatch
		var available int
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Price, &p.IsAvailable, &p.CreatedAt, &p.UpdatedAt, &available); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		p.Available = intToBool(available)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("product rows: %w", err)
	}
	return out, nil
}

// GetMenuProduct returns one product from the event's menu, including unlisted rows.
func (d *DB) GetMenuProduct(ctx context.Context, eventID string, productID int64) (*MenuProduct, error) {
	var p MenuProduct
	var available int
	err := d.sql.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.category, ep.price, ep.is_available, p.created_at, p.updated_at,
		       CASE WHEN p.is_available = 1 AND ep.is_available = 1
		                 AND NOT EXISTS (SELECT 1 FROM event_sold_out es
		                                  WHERE es.event_id = ? AND es.product_id = p.id)
		            THEN 1 ELSE 0 END AS available
		  FROM event_products ep
		  JOIN products p ON p.id = ep.product_id
		 WHERE ep.event_id = ? AND p.id = ?`, eventID, eventID, productID).
		Scan(&p.ID, &p.Name, &p.Category, &p.Price, &p.IsAvailable, &p.CreatedAt, &p.UpdatedAt, &available)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get menu product: %w", err)
	}
	p.Available = intToBool(available)

	rows, err := d.sql.QueryContext(ctx, `SELECT alias FROM product_aliases WHERE product_id = ? ORDER BY alias`, productID)
	if err != nil {
		return nil, fmt.Errorf("get aliases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		p.Aliases = append(p.Aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("alias rows: %w", err)
	}
	return &p, nil
}

// SetProductAvailability toggles per-event sold-out.
func (d *DB) SetProductAvailability(ctx context.Context, eventID string, productID int64, available bool) error {
	if _, err := d.GetEvent(ctx, eventID); err != nil {
		return err
	}
	if _, err := d.GetMenuProduct(ctx, eventID, productID); err != nil {
		return err
	}
	if available {
		_, err := d.sql.ExecContext(ctx, `DELETE FROM event_sold_out WHERE event_id = ? AND product_id = ?`, eventID, productID)
		if err != nil {
			return fmt.Errorf("mark available: %w", err)
		}
		return nil
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT OR IGNORE INTO event_sold_out (event_id, product_id, created_at) VALUES (?, ?, ?)`,
		eventID, productID, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("mark sold out: %w", err)
	}
	return nil
}
