package firstweek

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The review page in every language: the coach's text as written, the count,
// and the three ways on; no catalogue key reaches the page.
func TestTheReviewPageInEveryLanguage(t *testing.T) {
	want := map[users.Locale][]string{
		users.LocaleEN:   {"Your first week", "What next?", "Build a habit", "4 of 7 days done"},
		users.LocaleES:   {"Tu primera semana", "¿Y ahora qué?", "Crear un hábito", "4 de 7 días hechos"},
		users.LocalePTPT: {"A tua primeira semana", "E agora?", "Criar um hábito", "4 de 7 dias feitos"},
		users.LocalePTBR: {"Sua primeira semana", "E agora?", "Criar um hábito", "4 de 7 dias feitos"},
	}
	r := Review{
		Label: "Sleep better", Text: "Four nights of seven. Keep the 23:00 cut-off.", DaysDone: 4,
		Days: []bool{true, true, false, true, false, true, false},
	}
	user := users.User{ID: uuid.New(), DisplayName: "Ana", Email: "ana@north.test"}
	for locale, texts := range want {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		var b strings.Builder
		if err := ReviewPage(user, r).Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		html := b.String()
		for _, s := range append(texts, r.Text) {
			if !strings.Contains(html, s) {
				t.Errorf("%s: missing %q", locale, s)
			}
		}
		if strings.Contains(html, "firstweek.review.") || strings.Contains(html, "firstweek.card.") {
			t.Errorf("%s: a catalogue key reached the page", locale)
		}
		for _, href := range []string{`href="/app/care"`, `href="/app/training/plans"`, `href="/app/crews"`} {
			if !strings.Contains(html, href) {
				t.Errorf("%s: missing link %s", locale, href)
			}
		}
	}
}

// Day 7 offers the review beside the day's action; after day 7 there is
// nothing left to do but open it.
func TestTheCardOffersTheReviewFromDaySeven(t *testing.T) {
	days := []bool{true, false, true, true, false, false, false}
	seven := render(t, users.LocaleEN, Card{Show: true, Day: 7, Days: days, Action: "Write three lines.", ReviewReady: true})
	if !strings.Contains(seven, `href="/app/first-week/review"`) || !strings.Contains(seven, "first-week-mark-done") {
		t.Errorf("day 7 card = %s", seven)
	}

	over := render(t, users.LocaleES, Card{Show: true, Day: 9, Days: days, Action: "Write three lines.", ReviewReady: true, Over: true})
	if !strings.Contains(over, "Tu primera semana ha terminado") || !strings.Contains(over, `href="/app/first-week/review"`) {
		t.Errorf("after the week = %s", over)
	}
	if strings.Contains(over, "first-week-mark-done") || strings.Contains(over, "Write three lines.") {
		t.Error("the card still asks for an action after the week")
	}
}
