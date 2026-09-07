package goals

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderGoalsIn(t *testing.T, locale users.Locale, list []goal.Goal) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	user := users.User{DisplayName: "Ana", Locale: locale}
	page := IndexPage(user, list, Instruments{}, GoalForm{})
	if err := page.Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestGoalsRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Direction", "Add a goal", "Nothing to aim at yet"},
		users.LocalePTPT: {"Direção", "Adicionar objetivo", "Ainda não há nada para onde apontar"},
		users.LocalePTBR: {"Direção", "Adicionar objetivo", "Ainda não tem nada para mirar"},
		users.LocaleES:   {"Dirección", "Añadir un objetivo", "Todavía no hay nada a lo que apuntar"},
	}
	for locale, wants := range cases {
		html := renderGoalsIn(t, locale, nil)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// Categories and statuses are stored as lowercase identifiers, so the
// catalogue key is derived rather than switched on. Every offered category
// must resolve, or the select would show a raw key.
func TestEveryGoalCategoryIsTranslated(t *testing.T) {
	for _, locale := range users.Locales {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		for _, c := range goal.Categories {
			got := categoryLabel(ctx, c)
			if got == "" || strings.HasPrefix(got, "goal.category.") {
				t.Errorf("%s: category %q renders as %q", locale, c, got)
			}
		}
		for _, st := range []string{goal.StatusActive, goal.StatusAchieved, goal.StatusPaused, goal.StatusAbandoned} {
			got := statusLabel(ctx, st)
			if got == "" || strings.HasPrefix(got, "goal.status.") {
				t.Errorf("%s: status %q renders as %q", locale, st, got)
			}
		}
	}
}

// A stored value this build no longer knows must still read as something. The
// switches these replaced fell back to Other and Active, and so do they.
func TestUnknownCategoryAndStatusStillRender(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), string(users.LocaleES))

	if got := categoryLabel(ctx, "astrology"); got != "Otro" {
		t.Errorf("unknown category = %q, want the Other label", got)
	}
	if got := statusLabel(ctx, "vibing"); got != "Activo" {
		t.Errorf("unknown status = %q, want the Active label", got)
	}
}
