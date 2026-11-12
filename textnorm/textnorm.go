package textnorm

import (
	"strings"
	"unicode"
)

var honorifics = map[string]bool{
	"bu": true, "ibu": true,
	"pak": true, "bapak": true,
	"mas": true, "mbak": true, "mba": true, "kak": true,
	"bang": true, "om": true, "tante": true,
	"h": true, "hj": true,
}

func base(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// Name normalizes a customer name and strips leading honorifics.
func Name(s string) string {
	fields := strings.Fields(base(s))
	for len(fields) > 0 && honorifics[fields[0]] {
		fields = fields[1:]
	}
	return strings.Join(fields, " ")
}

// Term normalizes a product name or alias without stripping honorifics.
func Term(s string) string {
	return base(s)
}
