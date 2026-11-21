package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"bazaar-minggu/db"
)

func TestCreateStoreWithOrganizer(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	d := openTestDB(t)

	store, err := d.CreateStoreWithOrganizer(ctx, " warteg ", "Warteg Bu Tini", "whatsapp", "628111@s.whatsapp.net", now)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if store.Code != "WARTEG" {
		t.Fatalf("code = %q, want WARTEG", store.Code)
	}
	if store.Status != "active" {
		t.Fatalf("status = %q, want active", store.Status)
	}

	scope, err := d.ResolveScope(ctx, "whatsapp", "", "628111@s.whatsapp.net")
	if err != nil {
		t.Fatalf("resolve creator: %v", err)
	}
	if scope.StoreID != store.ID || scope.Role != db.RoleOrganizer || scope.Kind != db.PrincipalAccount {
		t.Fatalf("scope = %+v", scope)
	}
	if scope.AccountID == nil {
		t.Fatalf("account id missing")
	}

	if _, err := d.CreateStoreWithOrganizer(ctx, "WARTEG", "Again", "whatsapp", "628222@s.whatsapp.net", now); !errors.Is(err, db.ErrStoreCodeTaken) {
		t.Fatalf("duplicate code err = %v, want ErrStoreCodeTaken", err)
	}
}

func TestResolveScope(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	const (
		groupChat  = "120363041234567890@g.us"
		organizer  = "6281234567890@s.whatsapp.net"
		otherGroup = "120363049999999999@g.us"
	)

	setup := func(t *testing.T) *db.DB {
		t.Helper()
		d := openTestDB(t)
		// KPKM is store 1, created by the migration.
		addMember(t, d, 1, "whatsapp", organizer, "organizer")
		return d
	}

	tests := []struct {
		name     string
		prepare  func(t *testing.T, d *db.DB)
		chat     string
		sender   string
		wantErr  error
		wantKind db.PrincipalKind
		wantRole db.Role
	}{
		{
			name: "bound group",
			prepare: func(t *testing.T, d *db.DB) {
				t.Helper()
				if err := d.CreateGroupClaim(ctx, 1, db.RoleStaff, nil, "ABC123", now, now+900); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if _, err := d.RedeemGroupClaim(ctx, "ABC123", "whatsapp", groupChat, now); err != nil {
					t.Fatalf("redeem: %v", err)
				}
			},
			chat: groupChat, sender: "628777@s.whatsapp.net",
			wantKind: db.PrincipalGroup, wantRole: db.RoleStaff,
		},
		{
			name:    "organizer resolved in DM",
			prepare: func(t *testing.T, d *db.DB) {},
			chat:    organizer, sender: organizer,
			wantKind: db.PrincipalAccount, wantRole: db.RoleOrganizer,
		},
		{
			name:    "unbound chat and unknown sender",
			prepare: func(t *testing.T, d *db.DB) {},
			chat:    otherGroup, sender: "628999@s.whatsapp.net",
			wantErr: db.ErrNoStore,
		},
		{
			name: "revoked binding",
			prepare: func(t *testing.T, d *db.DB) {
				t.Helper()
				if err := d.CreateGroupClaim(ctx, 1, db.RoleStaff, nil, "REV123", now, now+900); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if _, err := d.RedeemGroupClaim(ctx, "REV123", "whatsapp", groupChat, now); err != nil {
					t.Fatalf("redeem: %v", err)
				}
				if _, err := d.SQL().Exec(
					`UPDATE store_groups SET status = 'revoked' WHERE chat_ref = ?`, groupChat); err != nil {
					t.Fatalf("revoke: %v", err)
				}
			},
			chat: groupChat, sender: organizer,
			wantErr: db.ErrNoStore,
		},
		{
			name: "suspended store",
			prepare: func(t *testing.T, d *db.DB) {
				t.Helper()
				if _, err := d.SQL().Exec(`UPDATE stores SET status = 'suspended' WHERE id = 1`); err != nil {
					t.Fatalf("suspend: %v", err)
				}
			},
			chat: organizer, sender: organizer,
			wantErr: db.ErrStoreSuspended,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := setup(t)
			tt.prepare(t, d)
			scope, err := d.ResolveScope(ctx, "whatsapp", tt.chat, tt.sender)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if scope.StoreID != 1 || scope.Kind != tt.wantKind || scope.Role != tt.wantRole {
				t.Fatalf("scope = %+v", scope)
			}
		})
	}
}

func TestGroupClaim(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	const chat = "120363041234567890@g.us"

	tests := []struct {
		name    string
		prepare func(t *testing.T, d *db.DB)
		code    string
		wantErr error
	}{
		{
			name:    "unknown code",
			prepare: func(t *testing.T, d *db.DB) {},
			code:    "ZZZZZZ",
			wantErr: db.ErrClaimInvalid,
		},
		{
			name: "expired code",
			prepare: func(t *testing.T, d *db.DB) {
				t.Helper()
				if err := d.CreateGroupClaim(ctx, 1, db.RoleStaff, nil, "EXP123", now-1000, now-1); err != nil {
					t.Fatalf("claim: %v", err)
				}
			},
			code:    "EXP123",
			wantErr: db.ErrClaimInvalid,
		},
		{
			name: "redeemed twice",
			prepare: func(t *testing.T, d *db.DB) {
				t.Helper()
				if err := d.CreateGroupClaim(ctx, 1, db.RoleStaff, nil, "ONE123", now, now+900); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if _, err := d.RedeemGroupClaim(ctx, "ONE123", "whatsapp", chat, now); err != nil {
					t.Fatalf("first redeem: %v", err)
				}
			},
			code:    "ONE123",
			wantErr: db.ErrClaimInvalid,
		},
		{
			name: "chat already bound",
			prepare: func(t *testing.T, d *db.DB) {
				t.Helper()
				for i, code := range []string{"FIRST1", "SECOND"} {
					if err := d.CreateGroupClaim(ctx, 1, db.RoleStaff, nil, code, now+int64(i), now+900); err != nil {
						t.Fatalf("claim %s: %v", code, err)
					}
				}
				if _, err := d.RedeemGroupClaim(ctx, "FIRST1", "whatsapp", chat, now); err != nil {
					t.Fatalf("first redeem: %v", err)
				}
			},
			code:    "SECOND",
			wantErr: db.ErrClaimInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := openTestDB(t)
			tt.prepare(t, d)
			_, err := d.RedeemGroupClaim(ctx, tt.code, "whatsapp", chat, now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRedeemGroupClaimActivatesBinding(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	const chat = "120363041234567890@g.us"
	d := openTestDB(t)

	if err := d.CreateGroupClaim(ctx, 1, db.RoleStaff, nil, "GOOD12", now, now+900); err != nil {
		t.Fatalf("claim: %v", err)
	}
	scope, err := d.RedeemGroupClaim(ctx, "GOOD12", "whatsapp", chat, now)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if scope.StoreID != 1 || scope.Role != db.RoleStaff || scope.Kind != db.PrincipalGroup {
		t.Fatalf("scope = %+v", scope)
	}

	var status string
	var used int64
	if err := d.SQL().QueryRow(
		`SELECT status FROM store_groups WHERE chat_ref = ?`, chat).Scan(&status); err != nil {
		t.Fatalf("read binding: %v", err)
	}
	if status != "active" {
		t.Fatalf("binding status = %q", status)
	}
	if err := d.SQL().QueryRow(
		`SELECT COALESCE(used_at, 0) FROM claims WHERE code = 'GOOD12'`).Scan(&used); err != nil {
		t.Fatalf("read claim: %v", err)
	}
	if used != now {
		t.Fatalf("used_at = %d, want %d", used, now)
	}
}

func TestRoleAllows(t *testing.T) {
	tests := []struct {
		name     string
		have     db.Role
		required db.Role
		want     bool
	}{
		{"organizer meets organizer", db.RoleOrganizer, db.RoleOrganizer, true},
		{"organizer meets staff", db.RoleOrganizer, db.RoleStaff, true},
		{"staff meets staff", db.RoleStaff, db.RoleStaff, true},
		{"staff cannot organize", db.RoleStaff, db.RoleOrganizer, false},
		{"unknown role is denied", "", db.RoleStaff, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.have.Allows(tt.required); got != tt.want {
				t.Fatalf("Allows(%q) = %v, want %v", tt.required, got, tt.want)
			}
		})
	}
}
