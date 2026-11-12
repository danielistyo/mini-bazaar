package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CreateEvent inserts an event. It does not activate it.
func (d *DB) CreateEvent(ctx context.Context, e Event) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO bazaar_events (id, name, event_date, cutoff_at, is_active, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.Name, e.EventDate, nullableInt(e.CutoffAt), boolToInt(e.IsActive), e.CreatedAt)
	if err != nil {
		return fmt.Errorf("create event: %w", err)
	}
	return nil
}

// GetEvent returns one event.
func (d *DB) GetEvent(ctx context.Context, id string) (*Event, error) {
	return scanEvent(d.sql.QueryRowContext(ctx,
		`SELECT id, name, event_date, cutoff_at, is_active, created_at
		 FROM bazaar_events WHERE id = ?`, id))
}

// GetActiveEvent returns the active event.
func (d *DB) GetActiveEvent(ctx context.Context) (*Event, error) {
	return scanEvent(d.sql.QueryRowContext(ctx,
		`SELECT id, name, event_date, cutoff_at, is_active, created_at
		 FROM bazaar_events WHERE is_active = 1 LIMIT 1`))
}

// ListEvents returns events newest first with order count and Omzet.
func (d *DB) ListEvents(ctx context.Context) ([]EventView, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT e.id, e.name, e.event_date, e.cutoff_at, e.is_active, e.created_at,
		       (SELECT COUNT(*) FROM orders o
		         WHERE o.event_id = e.id AND o.status = 'confirmed') AS order_count,
		       COALESCE((SELECT SUM(oi.quantity * oi.unit_price)
		                   FROM orders o JOIN order_items oi ON oi.order_id = o.id
		                  WHERE o.event_id = e.id AND o.status = 'confirmed'), 0) AS omzet
		  FROM bazaar_events e
		 ORDER BY e.event_date DESC, e.id`)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var out []EventView
	for rows.Next() {
		var e Event
		var active int
		var v EventView
		if err := rows.Scan(&e.ID, &e.Name, &e.EventDate, &e.CutoffAt, &active, &e.CreatedAt, &v.OrderCount, &v.Omzet); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.IsActive = intToBool(active)
		v.Event = e
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list events rows: %w", err)
	}
	return out, nil
}

// SetActiveEvent makes one event active and deactivates the rest.
func (d *DB) SetActiveEvent(ctx context.Context, id string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set active: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM bazaar_events WHERE id = ?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("find event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bazaar_events SET is_active = 0 WHERE is_active = 1`); err != nil {
		return fmt.Errorf("deactivate events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bazaar_events SET is_active = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("activate event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit set active: %w", err)
	}
	return nil
}

// EnsureEvent returns an existing event or creates it.
func (d *DB) EnsureEvent(ctx context.Context, e Event) (*Event, error) {
	existing, err := d.GetEvent(ctx, e.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if err := d.CreateEvent(ctx, e); err != nil {
		return nil, err
	}
	return d.GetEvent(ctx, e.ID)
}

func scanEvent(s scanner) (*Event, error) {
	var e Event
	var active int
	if err := s.Scan(&e.ID, &e.Name, &e.EventDate, &e.CutoffAt, &active, &e.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan event: %w", err)
	}
	e.IsActive = intToBool(active)
	return &e, nil
}
