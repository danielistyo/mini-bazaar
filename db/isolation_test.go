package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"bazaar-minggu/db"
)

// Every read is bounded by the store on the handle. Two stores with the same
// product and customer names must never see each other.
func TestReadsAreIsolatedByStore(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	d1 := openTestDB(t)

	const (
		event1 = "KPKM-EVENT"
		event2 = "WARTEG-EVENT"
		sate   = "Sate ayam"
	)

	if err := d1.CreateEvent(ctx, db.Event{ID: event1, Name: "KPKM", EventDate: "2026-11-15", CreatedAt: now}); err != nil {
		t.Fatalf("create event 1: %v", err)
	}
	if err := d1.SetActiveEvent(ctx, event1); err != nil {
		t.Fatalf("activate event 1: %v", err)
	}
	if _, err := d1.SeedProducts(ctx, event1, []db.MenuRow{
		{Category: "Makanan", Name: sate, Price: 35000},
	}); err != nil {
		t.Fatalf("seed store 1: %v", err)
	}

	store2, err := d1.CreateStoreWithOrganizer(ctx, "WARTEG", "Warteg", "whatsapp", "628222@s.whatsapp.net", now)
	if err != nil {
		t.Fatalf("create store 2: %v", err)
	}
	d2 := d1.ForStore(store2.ID)
	if err := d2.CreateEvent(ctx, db.Event{ID: event2, Name: "Warteg", EventDate: "2026-11-15", CreatedAt: now}); err != nil {
		t.Fatalf("create event 2: %v", err)
	}
	if err := d2.SetActiveEvent(ctx, event2); err != nil {
		t.Fatalf("activate event 2: %v", err)
	}
	if _, err := d2.SeedProducts(ctx, event2, []db.MenuRow{
		{Category: "Makanan", Name: sate, Price: 30000},
	}); err != nil {
		t.Fatalf("seed store 2: %v", err)
	}
	if _, err := d2.SQL().Exec(
		`INSERT INTO customers (store_id, display_name, normalized_name, created_at) VALUES (?, 'Susi', 'susi', ?)`,
		store2.ID, now); err != nil {
		t.Fatalf("insert store 2 customer: %v", err)
	}

	t.Run("events", func(t *testing.T) {
		got, err := d1.ListEvents(ctx)
		if err != nil {
			t.Fatalf("list store 1: %v", err)
		}
		if len(got) != 1 || got[0].ID != event1 {
			t.Fatalf("store 1 events = %+v", got)
		}
		got, err = d2.ListEvents(ctx)
		if err != nil {
			t.Fatalf("list store 2: %v", err)
		}
		if len(got) != 1 || got[0].ID != event2 {
			t.Fatalf("store 2 events = %+v", got)
		}
	})

	t.Run("active event", func(t *testing.T) {
		e1, err := d1.GetActiveEvent(ctx)
		if err != nil || e1.ID != event1 {
			t.Fatalf("store 1 active = %+v, %v", e1, err)
		}
		e2, err := d2.GetActiveEvent(ctx)
		if err != nil || e2.ID != event2 {
			t.Fatalf("store 2 active = %+v, %v", e2, err)
		}
	})

	t.Run("foreign event id", func(t *testing.T) {
		if _, err := d1.GetEvent(ctx, event2); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if err := d1.SetActiveEvent(ctx, event2); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("activate foreign event err = %v, want ErrNotFound", err)
		}
	})

	t.Run("menu", func(t *testing.T) {
		m1, err := d1.ListMenu(ctx, event1)
		if err != nil || len(m1) != 1 || m1[0].Price != 35000 {
			t.Fatalf("store 1 menu = %+v, %v", m1, err)
		}
		if foreign, err := d1.ListMenu(ctx, event2); err != nil || len(foreign) != 0 {
			t.Fatalf("store 1 sees foreign menu: %+v, %v", foreign, err)
		}
		if hits, err := d1.FindProducts(ctx, "sate", event2); err != nil || len(hits) != 0 {
			t.Fatalf("store 1 resolved a foreign product: %+v, %v", hits, err)
		}
		other, err := d2.ListMenu(ctx, event2)
		if err != nil || len(other) != 1 || other[0].Price != 30000 {
			t.Fatalf("store 2 menu = %+v, %v", other, err)
		}
		if _, err := d1.GetMenuProduct(ctx, event2, other[0].ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("foreign menu product err = %v, want ErrNotFound", err)
		}
	})

	t.Run("customers", func(t *testing.T) {
		if hits, err := d1.FindCustomers(ctx, "susi"); err != nil || len(hits) != 0 {
			t.Fatalf("store 1 sees foreign customer: %+v, %v", hits, err)
		}
		if hits, err := d2.FindCustomers(ctx, "susi"); err != nil || len(hits) != 1 {
			t.Fatalf("store 2 customers = %+v, %v", hits, err)
		}
	})

	t.Run("unscoped handle sees nothing", func(t *testing.T) {
		if got, err := d1.ForStore(0).ListEvents(ctx); err != nil || len(got) != 0 {
			t.Fatalf("unscoped events = %+v, %v", got, err)
		}
		if _, err := d1.ForStore(0).GetActiveEvent(ctx); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("unscoped active err = %v, want ErrNotFound", err)
		}
	})
}
