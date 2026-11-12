package format

import (
	"fmt"
	"time"
)

// WIB is the bazaar timezone.
var WIB = time.FixedZone("WIB", 7*60*60)

var days = [...]string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}

var months = [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

var shortMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// EventDate renders 2026-11-15 as "Minggu, 15 November 2026".
func EventDate(date string) string {
	t, err := time.ParseInLocation("2006-01-02", date, WIB)
	if err != nil {
		return date
	}
	return fmt.Sprintf("%s, %d %s %d", days[t.Weekday()], t.Day(), months[t.Month()-1], t.Year())
}

// ShortDate renders 2026-11-15 as "15 Nov 2026".
func ShortDate(date string) string {
	t, err := time.ParseInLocation("2006-01-02", date, WIB)
	if err != nil {
		return date
	}
	return fmt.Sprintf("%d %s %d", t.Day(), shortMonths[t.Month()-1], t.Year())
}

// DateTime renders a Unix timestamp in WIB.
func DateTime(unix int64) string {
	return time.Unix(unix, 0).In(WIB).Format("02 Jan 2006 15:04")
}
