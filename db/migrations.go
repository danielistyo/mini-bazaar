package db

import (
	"fmt"
)

func (d *DB) migrate() error {
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var version int
	if err := d.sql.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}

	migrations := []string{migrationV1, migrationV2}
	for i, m := range migrations {
		v := i + 1
		if v <= version {
			continue
		}
		tx, err := d.sql.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", v, err)
		}
		if _, err := tx.Exec(m); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, v); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", v, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", v, err)
		}
	}
	return nil
}

const migrationV1 = `
CREATE TABLE bazaar_events (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    event_date TEXT NOT NULL,
    cutoff_at INTEGER,
    is_active INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_one_active_event ON bazaar_events(is_active) WHERE is_active = 1;

CREATE TABLE products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    normalized_name TEXT NOT NULL UNIQUE,
    category TEXT NOT NULL,
    price INTEGER NOT NULL CHECK(price >= 0),
    is_available INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE product_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    UNIQUE(product_id, alias)
);
CREATE INDEX idx_product_aliases_norm ON product_aliases(normalized_alias);

CREATE TABLE event_sold_out (
    event_id TEXT NOT NULL REFERENCES bazaar_events(id) ON DELETE CASCADE,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(event_id, product_id)
);

CREATE TABLE customers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    display_name TEXT NOT NULL,
    normalized_name TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL REFERENCES bazaar_events(id),
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    order_number INTEGER NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('confirmed','cancelled')),
    is_paid INTEGER NOT NULL DEFAULT 0,
    paid_at INTEGER,
    notes TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    cancelled_at INTEGER,
    UNIQUE(event_id, order_number)
);
CREATE UNIQUE INDEX idx_one_confirmed_order ON orders(event_id, customer_id) WHERE status = 'confirmed';
CREATE INDEX idx_orders_event ON orders(event_id, status);
CREATE INDEX idx_orders_customer ON orders(customer_id);

CREATE TABLE order_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id INTEGER NOT NULL REFERENCES products(id),
    product_name TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK(quantity > 0),
    unit_price INTEGER NOT NULL CHECK(unit_price >= 0),
    note TEXT
);
CREATE INDEX idx_order_items_order ON order_items(order_id);
CREATE INDEX idx_order_items_product ON order_items(product_id);

CREATE TABLE order_audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    action TEXT NOT NULL,
    details TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_audit_order ON order_audit_logs(order_id);

CREATE TABLE drafts (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('create','edit','cancel')),
    event_id TEXT NOT NULL REFERENCES bazaar_events(id),
    order_id INTEGER REFERENCES orders(id),
    customer_id INTEGER REFERENCES customers(id),
    customer_name TEXT,
    payload TEXT NOT NULL,
    total INTEGER,
    status TEXT NOT NULL CHECK(status IN ('pending','committed','expired')),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    committed_at INTEGER
);
CREATE INDEX idx_drafts_status ON drafts(status);
CREATE INDEX idx_drafts_order ON drafts(order_id);
`

const migrationV2 = `
ALTER TABLE drafts ADD COLUMN short_code TEXT;
CREATE UNIQUE INDEX idx_drafts_short_code ON drafts(short_code);
`
