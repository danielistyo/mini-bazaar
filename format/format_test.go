package format_test

import (
	"testing"

	"bazaar-minggu/format"
)

func TestRupiah(t *testing.T) {
	tests := []struct {
		name   string
		amount int64
		want   string
	}{
		{"zero", 0, "Rp0"},
		{"hundreds", 999, "Rp999"},
		{"thousand", 1000, "Rp1.000"},
		{"forty three thousand", 43000, "Rp43.000"},
		{"one hundred five thousand", 105000, "Rp105.000"},
		{"million", 1000000, "Rp1.000.000"},
		{"negative", -105000, "-Rp105.000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := format.Rupiah(tt.amount); got != tt.want {
				t.Errorf("Rupiah(%d) = %q, want %q", tt.amount, got, tt.want)
			}
		})
	}
}

func TestEventDate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"sunday", "2026-11-15", "Minggu, 15 November 2026"},
		{"saturday", "2026-11-14", "Sabtu, 14 November 2026"},
		{"invalid", "not-a-date", "not-a-date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := format.EventDate(tt.in); got != tt.want {
				t.Errorf("EventDate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestShortDate(t *testing.T) {
	if got, want := format.ShortDate("2026-11-15"), "15 Nov 2026"; got != want {
		t.Errorf("ShortDate = %q, want %q", got, want)
	}
}
