package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bazaar-minggu/db"
)

// The upgrade path that runs on the live database: a V3 file with real rows must
// come out of V4 with every row owned by KPKM and no data lost.
func TestMigrationV4BackfillsExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")
	ctx := context.Background()
	now := time.Now().Unix()
	const eventID = "BAZAR-2026-11-15"

	v3, err := db.MigrateTo(path, 3)
	if err != nil {
		t.Fatalf("open v3: %v", err)
	}
	if _, err := v3.SQL().Exec(`
		INSERT INTO bazaar_events (id, name, event_date, is_active, created_at)
		VALUES (?, 'Bazar KPKM', '2026-11-15', 1, ?)`, eventID, now); err != nil {
		t.Fatalf("create v3 event: %v", err)
	}
	if _, err := v3.SQL().Exec(`
		INSERT INTO products (name, normalized_name, category, price, is_available, created_at, updated_at)
		VALUES ('Sate babi 5 tusuk', 'sate babi 5 tusuk', 'Makanan', 43000, 1, ?, ?)`,
		now, now); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := v3.SQL().Exec(`
		INSERT INTO event_products (event_id, product_id, price, is_available, created_at, updated_at)
		SELECT ?, id, 43000, 1, ?, ? FROM products WHERE name = 'Sate babi 5 tusuk'`,
		eventID, now, now); err != nil {
		t.Fatalf("seed menu: %v", err)
	}
	if _, err := v3.SQL().Exec(
		`INSERT INTO customers (display_name, normalized_name, created_at) VALUES ('Susi', 'susi', ?)`,
		now); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if err := v3.Close(); err != nil {
		t.Fatalf("close v3: %v", err)
	}

	upgraded, err := db.Open(path)
	if err != nil {
		t.Fatalf("upgrade to v4: %v", err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	d := upgraded.ForStore(1)

	var storeID int64
	if err := d.SQL().QueryRow(`SELECT id FROM stores WHERE code = 'KPKM'`).Scan(&storeID); err != nil {
		t.Fatalf("find store: %v", err)
	}
	if storeID != 1 {
		t.Fatalf("store id = %d, want 1", storeID)
	}

	checks := []struct {
		name  string
		query string
	}{
		{"events", `SELECT store_id FROM bazaar_events`},
		{"products", `SELECT store_id FROM products`},
		{"customers", `SELECT store_id FROM customers`},
	}
	for _, c := range checks {
		rows, err := d.SQL().Query(c.query)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		n := 0
		for rows.Next() {
			var got int64
			if err := rows.Scan(&got); err != nil {
				t.Fatalf("%s scan: %v", c.name, err)
			}
			if got != 1 {
				t.Fatalf("%s store_id = %d, want 1", c.name, got)
			}
			n++
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("%s close: %v", c.name, err)
		}
		if n == 0 {
			t.Fatalf("%s: no rows backfilled", c.name)
		}
	}

	menu, err := d.ListMenu(ctx, eventID)
	if err != nil {
		t.Fatalf("list menu after upgrade: %v", err)
	}
	if len(menu) != 1 || menu[0].Price != 43000 || menu[0].Name != "Sate babi 5 tusuk" {
		t.Fatalf("menu after upgrade = %+v", menu)
	}

	// A second event still copies the active menu, which is how a menu carries
	// over now that CSV seeding is on its way out.
	const copied = "EVENT-2026-12-20"
	if err := d.CreateEvent(ctx, db.Event{
		ID: copied, Name: "Bazar Test", EventDate: "2026-12-20", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create copied event: %v", err)
	}
	copiedMenu, err := d.ListMenu(ctx, copied)
	if err != nil {
		t.Fatalf("list copied menu: %v", err)
	}
	if len(copiedMenu) != 1 || copiedMenu[0].Price != 43000 {
		t.Fatalf("copied menu = %+v", copiedMenu)
	}
}

// The per-store unique keys are the point of V4: the same name can exist twice.
func TestSameProductNameInTwoStores(t *testing.T) {
	now := time.Now().Unix()
	d := openTestDB(t)
	d2 := d.ForStore(2)

	if _, err := d.SQL().Exec(
		`INSERT INTO stores (code, name, created_at) VALUES ('WARTEG', 'Warteg', ?)`, now); err != nil {
		t.Fatalf("insert second store: %v", err)
	}
	for _, store := range []*db.DB{d, d2} {
		if _, err := store.SQL().Exec(
			`INSERT INTO products (store_id, name, normalized_name, category, price, created_at, updated_at)
			 VALUES (?, 'Sate ayam', 'sate ayam', 'Makanan', 35000, ?, ?)`,
			store.StoreID(), now, now); err != nil {
			t.Fatalf("insert product for store %d: %v", store.StoreID(), err)
		}
	}

	var count int
	if err := d.SQL().QueryRow(`SELECT COUNT(*) FROM products WHERE name = 'Sate ayam'`).Scan(&count); err != nil {
		t.Fatalf("count products: %v", err)
	}
	if count != 2 {
		t.Fatalf("product count = %d, want 2", count)
	}

	var storeID int64
	if err := d.SQL().QueryRow(
		`SELECT store_id FROM products WHERE name = 'Sate ayam' AND store_id = 2`).Scan(&storeID); err != nil {
		t.Fatalf("store 2 product: %v", err)
	}
	if storeID != 2 {
		t.Fatalf("store id = %d, want 2", storeID)
	}
}
