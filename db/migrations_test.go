package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bazaar-minggu/db"
)

func TestMigrationV3BackfillsAndCopiesMenus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")
	ctx := context.Background()
	now := time.Now().Unix()
	const eventID = "BAZAR-2026-11-15"

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := d.CreateEvent(ctx, db.Event{
		ID:        eventID,
		Name:      "Bazar KPKM",
		EventDate: "2026-11-15",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if err := d.SetActiveEvent(ctx, eventID); err != nil {
		t.Fatalf("activate event: %v", err)
	}
	if _, err := d.SeedProducts(ctx, eventID, []db.MenuRow{
		{Category: "Makanan", Name: "Sate babi 5 tusuk", Price: 43000, Aliases: []string{"sate"}},
	}); err != nil {
		t.Fatalf("seed menu: %v", err)
	}

	if _, err := d.SQL().Exec(`DROP TABLE event_products`); err != nil {
		t.Fatalf("drop event_products: %v", err)
	}
	if _, err := d.SQL().Exec(`DELETE FROM schema_version WHERE version = 3`); err != nil {
		t.Fatalf("rewind schema version: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close before migration: %v", err)
	}

	d, err = db.Open(path)
	if err != nil {
		t.Fatalf("reopen for migration: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	menu, err := d.ListMenu(ctx, eventID)
	if err != nil {
		t.Fatalf("list backfilled menu: %v", err)
	}
	if len(menu) != 1 || menu[0].Name != "Sate babi 5 tusuk" || menu[0].Price != 43000 || !menu[0].Available {
		t.Fatalf("backfilled menu = %+v", menu)
	}

	const copiedEvent = "EVENT-2026-12-20"
	if err := d.CreateEvent(ctx, db.Event{
		ID:        copiedEvent,
		Name:      "Bazar Test",
		EventDate: "2026-12-20",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create copied event: %v", err)
	}
	copied, err := d.ListMenu(ctx, copiedEvent)
	if err != nil {
		t.Fatalf("list copied menu: %v", err)
	}
	if len(copied) != 1 || copied[0].Price != 43000 || !copied[0].Available {
		t.Fatalf("copied menu = %+v", copied)
	}
}
