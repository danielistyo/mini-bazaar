package textnorm_test

import (
	"testing"

	"bazaar-minggu/textnorm"
)

func TestName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Susi", "susi"},
		{"honorific", "Bu Susi", "susi"},
		{"multiple honorifics", "IBU HJ. SUSI", "susi"},
		{"punctuation", "  Bu   Ratna, S.Pd. ", "ratna s pd"},
		{"no honorific", "Budi Santoso", "budi santoso"},
		{"honorific only", "Bu", ""},
		{"empty", "", ""},
		{"hyphen", "Susi-Kantor", "susi kantor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textnorm.Name(tt.in); got != tt.want {
				t.Errorf("Name(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTerm(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Sate Babi 5 tusuk", "sate babi 5 tusuk"},
		{"punctuation", "Cincau (Orenji)", "cincau orenji"},
		{"honorific kept", "Bu Susi", "bu susi"},
		{"empty", "  ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textnorm.Term(tt.in); got != tt.want {
				t.Errorf("Term(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
