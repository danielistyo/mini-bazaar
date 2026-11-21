package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrDraftExpired      = errors.New("draft expired")
	ErrDraftNotPending   = errors.New("draft is not pending")
	ErrOrderNotConfirmed = errors.New("order is not confirmed")
)

// ExistingOrderError means the customer already has a confirmed order in the event.
type ExistingOrderError struct {
	OrderID int64
	Code    string
}

func (e *ExistingOrderError) Error() string {
	return "customer already has confirmed order " + e.Code
}

// SoldOutError means a product is unavailable for the event.
type SoldOutError struct {
	ProductName string
}

func (e *SoldOutError) Error() string {
	return "product sold out: " + e.ProductName
}

// DB wraps the SQLite connection. storeID is the tenant every query is bounded
// to; zero means unscoped, and unscoped queries return nothing.
type DB struct {
	sql     *sql.DB
	storeID int64
}

// ForStore returns a handle bounded to one store. It shares the connection.
func (d *DB) ForStore(id int64) *DB {
	c := *d
	c.storeID = id
	return &c
}

// StoreID is the store this handle is bounded to, zero when unscoped.
func (d *DB) StoreID() int64 { return d.storeID }

// Open opens the database, enables WAL, and runs every migration.
func Open(path string) (*DB, error) { return open(path, latestVersion) }

// MigrateTo opens the database but stops migrating at version. Tests use it to
// build an older schema before checking an upgrade.
func MigrateTo(path string, version int) (*DB, error) { return open(path, version) }

func open(path string, maxVersion int) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	conn := "file:" + path + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"
	sqlDB, err := sql.Open("sqlite3", conn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	d := &DB{sql: sqlDB}
	if err := d.migrateTo(maxVersion); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) SQL() *sql.DB { return d.sql }

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type scanner interface {
	Scan(dest ...any) error
}

func nullableInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool { return i != 0 }
