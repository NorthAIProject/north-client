package i18n

import (
	"context"
	"testing"
	"time"
)

func TestFormatTimeNamesMonthsAndWeekdaysInEachLocale(t *testing.T) {
	// A Monday in May: "May" is the same short and long in English, and "mai"
	// in Portuguese, so a swapped placeholder would show.
	when := time.Date(2026, time.May, 4, 9, 30, 0, 0, time.UTC)

	cases := []struct {
		tag, layout, want string
	}{
		{"en", "2 Jan", "4 May"},
		{"en", "Mon 2 Jan, 15:04", "Mon 4 May, 09:30"},
		{"en", "2 January 2006", "4 May 2026"},
		{"es", "2 Jan", "4 may"},
		{"es", "Mon 2 Jan", "lun 4 may"},
		{"es", "2 January 2006", "4 de mayo de 2026"},
		{"es", "Jan 2", "4 may"},
		{"pt-PT", "2 Jan 2006", "4 mai 2026"},
		{"pt-PT", "Mon 2 Jan, 15:04", "seg 4 mai, 09:30"},
		{"pt-PT", "2 January 2006", "4 de maio de 2026"},
		{"pt-BR", "Monday", "segunda-feira"},
		{"pt-BR", "Jan 2, 2006 3:04 PM", "4 mai 2026, 09:30"},
		{"fr", "2 Jan", "4 May"}, // not served: English, never a blank
	}
	for _, c := range cases {
		if got := FormatTime(c.tag, when, c.layout); got != c.want {
			t.Errorf("FormatTime(%s, %q) = %q, want %q", c.tag, c.layout, got, c.want)
		}
	}
}

func TestDateReadsTheRequestLocale(t *testing.T) {
	when := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	ctx := WithLocale(context.Background(), "es")
	if got := Date(ctx, when, "2 Jan"); got != "2 ene" {
		t.Errorf("Date = %q, want %q", got, "2 ene")
	}
	if got := Date(context.Background(), when, "2 Jan"); got != "2 Jan" {
		t.Errorf("Date without a locale = %q, want English", got)
	}
}
