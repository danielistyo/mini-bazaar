package format

import (
	"strconv"
	"strings"
)

// Rupiah formats an integer amount as Rp105.000.
func Rupiah(amount int64) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	raw := strconv.FormatInt(amount, 10)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString("Rp")
	for i, r := range raw {
		if i > 0 && (len(raw)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}
