package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"bazaar-minggu/db"
)

// openTestDB returns a handle bounded to KPKM, the store migration V4 creates.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d.ForStore(1)
}

// addMember gives an existing store a member reachable from one channel identity.
func addMember(t *testing.T, d *db.DB, storeID int64, channel, ref, role string) int64 {
	t.Helper()
	now := time.Now().Unix()
	res, err := d.SQL().Exec(`INSERT INTO accounts (created_at) VALUES (?)`, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("account id: %v", err)
	}
	if _, err := d.SQL().Exec(
		`INSERT INTO account_channels (account_id, channel, ref, status, created_at)
		 VALUES (?, ?, ?, 'active', ?)`, accountID, channel, ref, now); err != nil {
		t.Fatalf("link channel: %v", err)
	}
	if _, err := d.SQL().Exec(
		`INSERT INTO store_members (store_id, account_id, role, status, created_at, activated_at)
		 VALUES (?, ?, ?, 'active', ?, ?)`, storeID, accountID, role, now, now); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	return accountID
}
