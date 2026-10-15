package workouts

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Plans store their weekdays as English names ("Monday"), which is the
// vocabulary the plan validator and the week rotation parse. These name them
// in the reader's language. A label that is not a weekday — an imported plan
// can say "Day 1" — is shown as written.

// weekdayName is the full weekday, capitalised: it heads a card.
func weekdayName(ctx context.Context, label string) string {
	d, ok := parseWeekday(label)
	if !ok {
		return label
	}
	return capitalise(i18n.T(ctx, "weekday.long."+strconv.Itoa(int(d))))
}

// weekdayShort is the abbreviated weekday, for the week editor's toggles.
func weekdayShort(ctx context.Context, label string) string {
	d, ok := parseWeekday(label)
	if !ok {
		return label
	}
	return i18n.T(ctx, "weekday.short."+strconv.Itoa(int(d)))
}

func parseWeekday(label string) (time.Weekday, bool) {
	label = strings.TrimSpace(label)
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), label) {
			return d, true
		}
	}
	return 0, false
}

// capitalise upper-cases the first letter. Spanish and Portuguese write
// weekdays in lower case mid-sentence, but a heading starts with a capital.
func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
