package db

import (
	"fmt"
)

type migration struct {
	version int
	sql     string
	// rebuild runs with foreign keys off and checks them before commit, because
	// it drops and renames tables that other tables reference.
	rebuild bool
}

var migrations = []migration{
	{1, migrationV1, false},
	{2, migrationV2, false},
	{3, migrationV3, false},
	{4, migrationV4, true},
}

var latestVersion = migrations[len(migrations)-1].version

func (d *DB) migrateTo(maxVersion int) error {
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var version int
	if err := d.sql.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= version || m.version > maxVersion {
			continue
		}
		// foreign_keys cannot be toggled inside a transaction.
		if m.rebuild {
			if _, err := d.sql.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
				return fmt.Errorf("disable foreign keys: %w", err)
			}
		}
		err := d.applyMigration(m)
		if m.rebuild {
			if _, ferr := d.sql.Exec(`PRAGMA foreign_keys = ON`); ferr != nil && err == nil {
				err = fmt.Errorf("enable foreign keys: %w", ferr)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) applyMigration(m migration) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", m.version, err)
	}
	if _, err := tx.Exec(m.sql); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	if m.rebuild {
		rows, err := tx.Query(`PRAGMA foreign_key_check`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("check foreign keys %d: %w", m.version, err)
		}
		bad := rows.Next()
		if cerr := rows.Close(); cerr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("close foreign key check %d: %w", m.version, cerr)
		}
		if bad {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: foreign key violations", m.version)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", m.version, err)
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

const migrationV3 = `
CREATE TABLE event_products (
    event_id TEXT NOT NULL REFERENCES bazaar_events(id) ON DELETE CASCADE,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    price INTEGER NOT NULL CHECK(price >= 0),
    is_available INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (event_id, product_id)
);
CREATE INDEX idx_event_products_product ON event_products(product_id);

INSERT INTO event_products (event_id, product_id, price, is_available, created_at, updated_at)
SELECT e.id, p.id, p.price, p.is_available, p.created_at, p.updated_at
  FROM bazaar_events e
 CROSS JOIN products p;
`

// migrationV4 introduces stores. Existing rows belong to KPKM, the single store
// the bot serves today, so the upgrade keeps the live bazaar working untouched.
const migrationV4 = `
CREATE TABLE stores (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','suspended')),
    created_at INTEGER NOT NULL
);

INSERT INTO stores (code, name, created_at)
VALUES ('KPKM', 'Bazar KPKM', CAST(strftime('%s','now') AS INTEGER));

CREATE TABLE products_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    store_id INTEGER NOT NULL DEFAULT 1 REFERENCES stores(id),
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    category TEXT NOT NULL,
    price INTEGER NOT NULL CHECK(price >= 0),
    is_available INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(store_id, name),
    UNIQUE(store_id, normalized_name)
);
INSERT INTO products_new (id, store_id, name, normalized_name, category, price, is_available, created_at, updated_at)
SELECT id, 1, name, normalized_name, category, price, is_available, created_at, updated_at FROM products;
DROP TABLE products;
ALTER TABLE products_new RENAME TO products;

CREATE TABLE customers_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    store_id INTEGER NOT NULL DEFAULT 1 REFERENCES stores(id),
    display_name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE(store_id, normalized_name)
);
INSERT INTO customers_new (id, store_id, display_name, normalized_name, created_at)
SELECT id, 1, display_name, normalized_name, created_at FROM customers;
DROP TABLE customers;
ALTER TABLE customers_new RENAME TO customers;

ALTER TABLE bazaar_events ADD COLUMN store_id INTEGER NOT NULL DEFAULT 1 REFERENCES stores(id);
DROP INDEX idx_one_active_event;
CREATE UNIQUE INDEX idx_one_active_event ON bazaar_events(store_id) WHERE is_active = 1;
CREATE INDEX idx_events_store ON bazaar_events(store_id);

CREATE TABLE accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at INTEGER NOT NULL
);

CREATE TABLE account_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    channel TEXT NOT NULL CHECK(channel IN ('whatsapp','telegram')),
    ref TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','revoked')),
    created_at INTEGER NOT NULL,
    UNIQUE(channel, ref)
);
CREATE INDEX idx_account_channels_account ON account_channels(account_id);

CREATE TABLE store_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    store_id INTEGER NOT NULL REFERENCES stores(id),
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    role TEXT NOT NULL CHECK(role IN ('organizer','staff')),
    status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','revoked')),
    created_at INTEGER NOT NULL,
    activated_at INTEGER,
    revoked_at INTEGER
);
CREATE UNIQUE INDEX idx_one_active_member
    ON store_members(account_id) WHERE status = 'active';
CREATE INDEX idx_store_members_store ON store_members(store_id);

CREATE TABLE store_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    store_id INTEGER NOT NULL REFERENCES stores(id),
    channel TEXT NOT NULL CHECK(channel IN ('whatsapp','telegram')),
    chat_ref TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('organizer','staff')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','active','revoked')),
    granted_by INTEGER REFERENCES accounts(id),
    created_at INTEGER NOT NULL,
    activated_at INTEGER,
    revoked_at INTEGER
);
CREATE UNIQUE INDEX idx_one_active_group
    ON store_groups(channel, chat_ref) WHERE status = 'active';
CREATE INDEX idx_store_groups_store ON store_groups(store_id);

CREATE TABLE claims (
    code TEXT PRIMARY KEY,
    purpose TEXT NOT NULL CHECK(purpose IN ('bind_group','link_channel','invite_member')),
    store_id INTEGER REFERENCES stores(id),
    chat_ref TEXT,
    account_id INTEGER REFERENCES accounts(id),
    role TEXT,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER
);
CREATE INDEX idx_claims_expiry ON claims(expires_at);
`
