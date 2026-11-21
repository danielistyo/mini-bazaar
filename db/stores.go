package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrNoStore means the caller resolves to no store. There is no fallback.
	ErrNoStore = errors.New("caller is not connected to a store")
	// ErrStoreSuspended means the store exists but is switched off.
	ErrStoreSuspended = errors.New("store is suspended")
	// ErrClaimInvalid covers unknown, expired, used and wrong-purpose codes.
	ErrClaimInvalid = errors.New("claim code is invalid")
	// ErrStoreCodeTaken means the store code is already registered.
	ErrStoreCodeTaken = errors.New("store code already exists")
	// ErrAlreadyMember means the caller already belongs to a store.
	ErrAlreadyMember = errors.New("caller already belongs to a store")
)

// Role is how much of a store a caller may operate.
type Role string

const (
	RoleOrganizer Role = "organizer"
	RoleStaff     Role = "staff"
)

// Allows reports whether r meets the minimum role required by a tool.
func (r Role) Allows(required Role) bool {
	if required == RoleOrganizer {
		return r == RoleOrganizer
	}
	return r == RoleOrganizer || r == RoleStaff
}

// PrincipalKind says how a caller was identified.
type PrincipalKind string

const (
	PrincipalGroup   PrincipalKind = "group"
	PrincipalAccount PrincipalKind = "account"
)

// Scope is the store, role and caller one message acts under.
type Scope struct {
	StoreID   int64
	Role      Role
	Kind      PrincipalKind
	AccountID *int64
	Channel   string
	ChatRef   string
}

// Store is one tenant.
type Store struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

const storeColumns = `id, code, name, status, created_at`

// GetStore returns one store by id.
func (d *DB) GetStore(ctx context.Context, id int64) (*Store, error) {
	return scanStore(d.sql.QueryRowContext(ctx,
		`SELECT `+storeColumns+` FROM stores WHERE id = ?`, id))
}

// GetStoreByCode returns one store by its code.
func (d *DB) GetStoreByCode(ctx context.Context, code string) (*Store, error) {
	return scanStore(d.sql.QueryRowContext(ctx,
		`SELECT `+storeColumns+` FROM stores WHERE code = ?`, strings.ToUpper(strings.TrimSpace(code))))
}

// CreateStoreWithOrganizer creates a store and makes the caller its organizer.
// The caller is identified by the channel it arrives on, so no account is needed
// to bootstrap a store.
func (d *DB) CreateStoreWithOrganizer(ctx context.Context, code, name, channel, senderRef string, now int64) (*Store, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, fmt.Errorf("store code and name are required")
	}
	if senderRef == "" {
		return nil, ErrNoStore
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create store: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM stores WHERE code = ?`, code).Scan(&exists)
	switch {
	case err == nil:
		return nil, ErrStoreCodeTaken
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("check store code: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO stores (code, name, status, created_at) VALUES (?, ?, 'active', ?)`,
		code, name, now)
	if err != nil {
		return nil, fmt.Errorf("insert store: %w", err)
	}
	storeID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store id: %w", err)
	}

	// One store per person: an identity that already has an active membership
	// cannot create another store.
	var member int
	err = tx.QueryRowContext(ctx, `
		SELECT 1 FROM account_channels ac
		  JOIN store_members m ON m.account_id = ac.account_id
		 WHERE ac.channel = ? AND ac.ref = ? AND ac.status = 'active' AND m.status = 'active'`,
		channel, senderRef).Scan(&member)
	switch {
	case err == nil:
		return nil, ErrAlreadyMember
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("check existing membership: %w", err)
	}

	var accountID int64
	err = tx.QueryRowContext(ctx,
		`SELECT account_id FROM account_channels WHERE channel = ? AND ref = ?`,
		channel, senderRef).Scan(&accountID)
	switch {
	case err == nil:
		// identity exists but has no active store; reuse it
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `INSERT INTO accounts (created_at) VALUES (?)`, now)
		if err != nil {
			return nil, fmt.Errorf("insert account: %w", err)
		}
		accountID, err = res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("account id: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO account_channels (account_id, channel, ref, status, created_at)
			 VALUES (?, ?, ?, 'active', ?)`, accountID, channel, senderRef, now); err != nil {
			return nil, fmt.Errorf("link channel identity: %w", err)
		}
	default:
		return nil, fmt.Errorf("read channel identity: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO store_members (store_id, account_id, role, status, created_at, activated_at)
		 VALUES (?, ?, 'organizer', 'active', ?, ?)`, storeID, accountID, now, now); err != nil {
		return nil, fmt.Errorf("insert organizer: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create store: %w", err)
	}
	return d.GetStore(ctx, storeID)
}

// ResolveScope maps a caller to a store and a role. A bound chat wins over the
// sender's own membership, so a group always acts as the group.
//
// Whether a chat is a group cannot be read from the refs: WhatsApp reports a
// direct message under a LID chat id that differs from the sender's JID. The
// channel allowlist decides where the bot listens at all, which is what keeps an
// unbound group out.
func (d *DB) ResolveScope(ctx context.Context, channel, chatRef, senderRef string) (*Scope, error) {
	if chatRef != "" {
		var storeID int64
		var role string
		err := d.sql.QueryRowContext(ctx, `
			SELECT store_id, role FROM store_groups
			 WHERE channel = ? AND chat_ref = ? AND status = 'active'`,
			channel, chatRef).Scan(&storeID, &role)
		switch {
		case err == nil:
			if err := d.requireActiveStore(ctx, storeID); err != nil {
				return nil, err
			}
			return &Scope{StoreID: storeID, Role: Role(role), Kind: PrincipalGroup,
				Channel: channel, ChatRef: chatRef}, nil
		case errors.Is(err, sql.ErrNoRows):
			// not a bound chat; fall through to the sender
		default:
			return nil, fmt.Errorf("resolve group binding: %w", err)
		}
	}

	if senderRef == "" {
		return nil, ErrNoStore
	}
	var accountID, storeID int64
	var role string
	err := d.sql.QueryRowContext(ctx, `
		SELECT ac.account_id, m.store_id, m.role
		  FROM account_channels ac
		  JOIN store_members m ON m.account_id = ac.account_id
		 WHERE ac.channel = ? AND ac.ref = ?
		   AND ac.status = 'active' AND m.status = 'active'`,
		channel, senderRef).Scan(&accountID, &storeID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoStore
	}
	if err != nil {
		return nil, fmt.Errorf("resolve account: %w", err)
	}
	if err := d.requireActiveStore(ctx, storeID); err != nil {
		return nil, err
	}
	return &Scope{StoreID: storeID, Role: Role(role), Kind: PrincipalAccount,
		AccountID: &accountID, Channel: channel, ChatRef: chatRef}, nil
}

func (d *DB) requireActiveStore(ctx context.Context, id int64) error {
	store, err := d.GetStore(ctx, id)
	if err != nil {
		return err
	}
	if store.Status != "active" {
		return ErrStoreSuspended
	}
	return nil
}

// CreateGroupClaim issues a one-time code that binds a chat to a store.
func (d *DB) CreateGroupClaim(ctx context.Context, storeID int64, role Role, accountID *int64, code string, now, expiresAt int64) error {
	if _, err := d.GetStore(ctx, storeID); err != nil {
		return err
	}
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO claims (code, purpose, store_id, account_id, role, created_at, expires_at)
		VALUES (?, 'bind_group', ?, ?, ?, ?, ?)`,
		code, storeID, nullableInt(accountID), string(role), now, expiresAt); err != nil {
		return fmt.Errorf("insert claim: %w", err)
	}
	return nil
}

// RedeemGroupClaim binds chatRef to the store named by the code. A code works
// once; unknown, expired and used codes fail the same way.
func (d *DB) RedeemGroupClaim(ctx context.Context, code, channel, chatRef string, now int64) (*Scope, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin redeem claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var storeID int64
	var role sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT store_id, role FROM claims
		 WHERE code = ? AND purpose = 'bind_group' AND used_at IS NULL AND expires_at > ?`,
		code, now).Scan(&storeID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrClaimInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("read claim: %w", err)
	}
	if !role.Valid || role.String == "" {
		return nil, ErrClaimInvalid
	}
	if chatRef == "" {
		return nil, ErrClaimInvalid
	}

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM stores WHERE id = ?`, storeID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClaimInvalid
		}
		return nil, fmt.Errorf("read store status: %w", err)
	}
	if status != "active" {
		return nil, ErrStoreSuspended
	}

	var bound int
	err = tx.QueryRowContext(ctx, `
		SELECT 1 FROM store_groups
		 WHERE channel = ? AND chat_ref = ? AND status = 'active'`, channel, chatRef).Scan(&bound)
	switch {
	case err == nil:
		return nil, ErrClaimInvalid
	case !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("check binding: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE claims SET used_at = ? WHERE code = ?`, now, code); err != nil {
		return nil, fmt.Errorf("consume claim: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO store_groups (store_id, channel, chat_ref, role, status, created_at, activated_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?)`,
		storeID, channel, chatRef, role.String, now, now); err != nil {
		return nil, fmt.Errorf("bind group: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit redeem claim: %w", err)
	}

	return &Scope{StoreID: storeID, Role: Role(role.String), Kind: PrincipalGroup,
		Channel: channel, ChatRef: chatRef}, nil
}

func scanStore(s scanner) (*Store, error) {
	var st Store
	if err := s.Scan(&st.ID, &st.Code, &st.Name, &st.Status, &st.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan store: %w", err)
	}
	return &st, nil
}
