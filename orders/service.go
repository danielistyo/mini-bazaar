package orders

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"bazaar-minggu/db"
	"bazaar-minggu/format"
	"bazaar-minggu/textnorm"
)

const draftTTL = 30 * time.Minute

// Service owns the order rules and formats tool output.
type Service struct {
	db *db.DB
}

func New(d *db.DB) *Service { return &Service{db: d} }

// Error is a business error the LLM can explain to the user.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errf(code, f string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(f, args...)}
}

// EventResult is one event plus a message.
type EventResult struct {
	Event     *db.Event `json:"event"`
	Formatted string    `json:"formatted"`
}

// EventListView is the event list plus a message.
type EventListView struct {
	Events    []db.EventView `json:"events"`
	Formatted string         `json:"formatted"`
}

// MenuView is the menu plus a message.
type MenuView struct {
	Event     *db.Event        `json:"event"`
	Products  []db.MenuProduct `json:"products"`
	Formatted string           `json:"formatted"`
}

// ProductMatchesView is a product lookup.
type ProductMatchesView struct {
	Products  []db.ProductMatch `json:"products"`
	Formatted string            `json:"formatted"`
}

// CustomerMatchesView is a customer lookup.
type CustomerMatchesView struct {
	Customers []db.CustomerMatch `json:"customers"`
	Formatted string             `json:"formatted"`
}

// MenuProductView is one product after a sold-out toggle.
type MenuProductView struct {
	Product   *db.MenuProduct `json:"product"`
	Formatted string          `json:"formatted"`
}

// OrderView is an order plus a receipt message.
type OrderView struct {
	Order     *db.OrderDetail `json:"order"`
	Formatted string          `json:"formatted"`
}

// SearchView is a compact order search result.
type SearchView struct {
	Orders    []db.OrderRow `json:"orders"`
	Formatted string        `json:"formatted"`
}

// SummaryView is the event report.
type SummaryView struct {
	Summary   *db.Summary `json:"summary"`
	Formatted string      `json:"formatted"`
}

func (s *Service) resolveEvent(ctx context.Context, eventID string) (*db.Event, error) {
	if strings.TrimSpace(eventID) != "" {
		e, err := s.db.GetEvent(ctx, eventID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, errf("not_found", "Event %s tidak ditemukan.", eventID)
			}
			return nil, err
		}
		return e, nil
	}
	e, err := s.db.GetActiveEvent(ctx)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("no_active_event", "Belum ada event aktif.")
		}
		return nil, err
	}
	return e, nil
}

func (s *Service) mapDBError(err error) error {
	if err == nil {
		return nil
	}
	var eo *db.ExistingOrderError
	if errors.As(err, &eo) {
		return errf("existing_order", "Customer sudah punya order %s. Tambahkan item ke order itu?", eo.Code)
	}
	var so *db.SoldOutError
	if errors.As(err, &so) {
		return errf("sold_out", "%s sedang habis.", so.ProductName)
	}
	switch {
	case errors.Is(err, db.ErrNotFound):
		return errf("not_found", "Data tidak ditemukan.")
	case errors.Is(err, db.ErrDraftExpired):
		return errf("draft_expired", "Draft sudah kedaluwarsa. Buat draft baru.")
	case errors.Is(err, db.ErrOrderNotConfirmed):
		return errf("order_cancelled", "Order sudah dibatalkan.")
	}
	return err
}

// ListEvents returns all events.
func (s *Service) ListEvents(ctx context.Context) (*EventListView, error) {
	events, err := s.db.ListEvents(ctx)
	if err != nil {
		return nil, err
	}
	return &EventListView{Events: events, Formatted: format.Events(events)}, nil
}

// CreateEvent creates an inactive event.
func (s *Service) CreateEvent(ctx context.Context, name, eventDate, cutoffAt, code string) (*EventResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errf("invalid_input", "Nama event wajib diisi.")
	}
	if _, err := time.ParseInLocation("2006-01-02", eventDate, format.WIB); err != nil {
		return nil, errf("invalid_input", "Tanggal event harus YYYY-MM-DD.")
	}
	if code == "" {
		code = "EVENT-" + eventDate
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !validEventCode(code) {
		return nil, errf("invalid_input", "Kode event hanya boleh huruf besar, angka, dan tanda minus.")
	}
	cutoff, err := parseCutoff(cutoffAt)
	if err != nil {
		return nil, err
	}

	e := db.Event{
		ID:        code,
		Name:      name,
		EventDate: eventDate,
		CutoffAt:  cutoff,
		IsActive:  false,
		CreatedAt: time.Now().Unix(),
	}
	if err := s.db.CreateEvent(ctx, e); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, errf("conflict", "Event %s sudah ada.", code)
		}
		return nil, err
	}
	return &EventResult{Event: &e, Formatted: format.EventCreated(&e)}, nil
}

// SetActiveEvent switches the active event.
func (s *Service) SetActiveEvent(ctx context.Context, eventID string) (*EventResult, error) {
	if strings.TrimSpace(eventID) == "" {
		return nil, errf("invalid_input", "Kode event wajib diisi.")
	}
	if err := s.db.SetActiveEvent(ctx, eventID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Event %s tidak ditemukan.", eventID)
		}
		return nil, err
	}
	e, err := s.db.GetEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	return &EventResult{Event: e, Formatted: format.EventActivated(e)}, nil
}

// EnsureInitialEvent seeds BAZAR-2026-11-15 and activates it only when no event is active.
func EnsureInitialEvent(ctx context.Context, d *db.DB) error {
	const code = "BAZAR-2026-11-15"
	cutoff := time.Date(2026, 11, 14, 12, 0, 0, 0, format.WIB).Unix()

	e, err := d.GetEvent(ctx, code)
	if errors.Is(err, db.ErrNotFound) {
		e = &db.Event{
			ID:        code,
			Name:      "Bazar KPKM",
			EventDate: "2026-11-15",
			CutoffAt:  &cutoff,
			CreatedAt: time.Now().Unix(),
		}
		if err := d.CreateEvent(ctx, *e); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if _, err := d.GetActiveEvent(ctx); errors.Is(err, db.ErrNotFound) {
		return d.SetActiveEvent(ctx, e.ID)
	} else if err != nil {
		return err
	}
	return nil
}

// GetMenu returns the menu for an event.
func (s *Service) GetMenu(ctx context.Context, eventID string) (*MenuView, error) {
	e, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	products, err := s.db.ListMenu(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	return &MenuView{Event: e, Products: products, Formatted: format.Menu(e, products)}, nil
}

// FindProduct returns product candidates.
func (s *Service) FindProduct(ctx context.Context, term, eventID string) (*ProductMatchesView, error) {
	e, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(term) == "" {
		return nil, errf("invalid_input", "Masukkan nama produk.")
	}
	matches, err := s.db.FindProducts(ctx, term, e.ID)
	if err != nil {
		return nil, err
	}
	return &ProductMatchesView{Products: matches, Formatted: format.ProductMatches(matches)}, nil
}

// SetProductAvailable toggles per-event sold-out.
func (s *Service) SetProductAvailable(ctx context.Context, productID int64, available bool, eventID string) (*MenuProductView, error) {
	e, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if err := s.db.SetProductAvailability(ctx, e.ID, productID, available); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Produk %d tidak ditemukan.", productID)
		}
		return nil, err
	}
	p, err := s.db.GetMenuProduct(ctx, e.ID, productID)
	if err != nil {
		return nil, err
	}
	return &MenuProductView{Product: p, Formatted: format.ProductAvailability(e, p, available)}, nil
}

// FindCustomer returns customer candidates.
func (s *Service) FindCustomer(ctx context.Context, name string) (*CustomerMatchesView, error) {
	normalized := textnorm.Name(name)
	if normalized == "" {
		return nil, errf("invalid_input", "Masukkan nama customer.")
	}
	matches, err := s.db.FindCustomers(ctx, normalized)
	if err != nil {
		return nil, err
	}
	return &CustomerMatchesView{Customers: matches, Formatted: format.CustomerMatches(matches)}, nil
}

func parseCutoff(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02 15:04", s, format.WIB)
	}
	if err != nil {
		return nil, errf("invalid_input", "Cutoff harus RFC3339 atau YYYY-MM-DD HH:MM.")
	}
	u := t.Unix()
	return &u, nil
}

func validEventCode(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func newDraftID() (string, error) {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = crockford[int(b)%len(crockford)]
	}
	return "D-" + string(out), nil
}

func newShortCode() (string, error) {
	var raw [2]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	n := int(raw[0])<<8 | int(raw[1])
	return fmt.Sprintf("%04d", n%10000), nil
}

func (s *Service) storeDraft(ctx context.Context, draft db.Draft) (string, string, error) {
	var last error
	for i := 0; i < 5; i++ {
		id, err := newDraftID()
		if err != nil {
			return "", "", err
		}
		code, err := newShortCode()
		if err != nil {
			return "", "", err
		}
		draft.ID = id
		draft.ShortCode = code
		if err := s.db.CreateDraft(ctx, draft); err != nil {
			last = err
			if !strings.Contains(strings.ToLower(err.Error()), "unique") {
				return "", "", err
			}
			continue
		}
		return id, code, nil
	}
	return "", "", last
}
