package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bazaar-minggu/db"
	"bazaar-minggu/format"
)

// ProductView is a menu product plus a WhatsApp-ready confirmation.
type ProductView struct {
	Product   *db.Product `json:"product"`
	Formatted string      `json:"formatted"`
}

// UpsertProduct adds or updates a product on one event's menu.
func (s *Service) UpsertProduct(ctx context.Context, eventID string, productID int64, name, category string, price int64, aliases *[]string, available *bool) (*ProductView, error) {
	e, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	category = strings.TrimSpace(category)
	if name == "" {
		return nil, errf("invalid_input", "Nama produk wajib diisi.")
	}
	if category == "" {
		return nil, errf("invalid_input", "Kategori produk wajib diisi.")
	}
	if price < 0 {
		return nil, errf("invalid_input", "Harga produk tidak boleh negatif.")
	}

	product, err := s.db.UpsertProduct(ctx, db.ProductInput{
		EventID:   e.ID,
		ProductID: productID,
		Name:      name,
		Category:  category,
		Price:     price,
		Aliases:   aliases,
		Available: available,
	})
	if err != nil {
		var conflict *db.ProductConflictError
		if errors.As(err, &conflict) {
			return nil, errf("conflict", "Produk %s sudah ada (id %d). Kirim product_id untuk mengubahnya.", conflict.Name, conflict.ProductID)
		}
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Produk tidak ditemukan.")
		}
		return nil, err
	}

	action := "ditambahkan ke"
	if productID > 0 {
		action = "diperbarui di"
	}
	return &ProductView{
		Product: product,
		Formatted: fmt.Sprintf("✅ Produk %s %s %s — %s — %s.",
			product.Name, action, e.Name, product.Category, format.Rupiah(product.Price)),
	}, nil
}

// SetProductEnabled lists or removes a product on one event's menu.
func (s *Service) SetProductEnabled(ctx context.Context, eventID string, productID int64, enabled bool) (*ProductView, error) {
	e, err := s.resolveEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	product, err := s.db.SetProductEnabled(ctx, e.ID, productID, enabled)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errf("not_found", "Produk tidak ditemukan di menu event %s.", e.Name)
		}
		return nil, err
	}

	state := "dinonaktifkan dari menu"
	if enabled {
		state = "diaktifkan di menu"
	}
	return &ProductView{
		Product:   product,
		Formatted: fmt.Sprintf("✅ Produk %s %s %s.", product.Name, state, e.Name),
	}, nil
}
