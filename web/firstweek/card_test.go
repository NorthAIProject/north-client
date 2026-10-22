package firstweek

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/moments"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func render(t *testing.T, locale users.Locale, c Card) string {
	t.Helper()
	ctx := i18n.WithLocale(context.Background(), string(locale))
	var b strings.Builder
	if err := Fragment(c).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

// Every locale names the day and offers Done; none falls back to a raw key.
func TestTheDayCardInEveryLanguage(t *testing.T) {
	want := map[users.Locale]string{
		users.LocaleEN:   "Day 3 of 7",
		users.LocaleES:   "Día 3 de 7",
		users.LocalePTPT: "Dia 3 de 7",
		users.LocalePTBR: "Dia 3 de 7",
	}
	c := Card{Show: true, Day: 3, Days: []bool{true, true, false, false, false, false, false}, Action: "Write three lines."}
	for locale, title := range want {
		html := render(t, locale, c)
		if !strings.Contains(html, title) {
			t.Errorf("%s: missing %q", locale, title)
		}
		if strings.Contains(html, "firstweek.card.") {
			t.Errorf("%s: a catalogue key reached the page", locale)
		}
		if !strings.Contains(html, `data-testid="first-week-mark-done"`) || !strings.Contains(html, `hx-post="/app/first-week/done"`) {
			t.Errorf("%s: no Done button", locale)
		}
	}
}

func TestADoneDayShowsNoButtonAndTheMomentShows(t *testing.T) {
	m := moments.ForFirstWeekThree(context.Background())
	html := render(t, users.LocaleEN, Card{Show: true, Day: 3, Done: true, Days: []bool{true, true, true, false, false, false, false}, Action: "x", Moment: &m})
	if strings.Contains(html, "first-week-mark-done") {
		t.Error("a done day still offers Done")
	}
	if !strings.Contains(html, "Done for today.") || !strings.Contains(html, "3 of 3 days") {
		t.Errorf("done card = %s", html)
	}
	if got := strings.Count(html, `data-done="true"`); got != 3 {
		t.Errorf("filled dots = %d, want 3", got)
	}
}

func TestDayZeroPreviewsTheActionWithoutDone(t *testing.T) {
	html := render(t, users.LocaleEN, Card{Show: true, Day: 0, Days: make([]bool, 7), Action: "Write three lines."})
	if !strings.Contains(html, "Your first week starts tomorrow") || strings.Contains(html, "first-week-mark-done") {
		t.Errorf("day 0 card = %s", html)
	}
}

func TestNoWeekRendersOnlyTheAnchor(t *testing.T) {
	html := render(t, users.LocaleEN, Card{})
	if strings.Contains(html, "first-week-card\"") && strings.Contains(html, "data-testid") {
		t.Errorf("hidden card rendered content: %s", html)
	}
	if !strings.Contains(html, `id="first-week-card"`) {
		t.Error("the swap anchor is missing")
	}
}
