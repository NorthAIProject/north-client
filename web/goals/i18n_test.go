package goals

import (
	"context"
	"strings"
	"testing"
	"time"

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
			if got == "" || strings.HasPrefix(got, "lifedomain.") {
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

func renderDetailIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	g := goal.Goal{
		Title:          "Run 10k",
		Motivation:     "Breathe on the stairs",
		Success:        "10k without walking",
		Category:       goal.CategoryOther,
		Status:         goal.StatusActive,
		TargetDate:     time.Now().AddDate(0, 0, 10),
		MilestoneDone:  1,
		MilestoneTotal: 3,
		Milestones: []goal.Milestone{
			{Title: "5k", Status: goal.MilestoneOpen, TargetDate: time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC)},
		},
	}
	updates := []goal.Update{{Note: "Ran twice", CreatedAt: time.Date(2026, time.March, 4, 9, 30, 0, 0, time.UTC)}}

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	page := DetailPage(users.User{DisplayName: "Ana", Locale: locale}, g, updates, GoalForm{}, nil)
	if err := page.Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

// The detail page carries the most hand-written copy: the back link, the
// milestone count, the note form and the dates. All of it follows the locale.
func TestGoalDetailRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"All goals", "Why it matters", "1 of 3 complete.", "Add milestone", "Add note", "4 Mar 2026", "4 March 2026, 09:30", " days"},
		users.LocalePTPT: {"Todos os objetivos", "Porque importa", "1 de 3 concluídos.", "Adicionar marco", "Adicionar nota", "faltam "},
		users.LocalePTBR: {"Todos os objetivos", "Por que importa", "1 de 3 concluídos.", "Adicionar marco", "Adicionar nota", "faltam "},
		users.LocaleES:   {"Todos los objetivos", "Por qué importa", "1 de 3 completados.", "Añadir hito", "Añadir nota", "faltan ", "mar"},
	}
	for locale, wants := range cases {
		html := renderDetailIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: detail page does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedGoalDetail(t *testing.T) {
	englishOnly := []string{"All goals", "Why it matters", "Done when", "complete.", "Add milestone", "Add note", "Remove this milestone?", "This cannot be undone", "March"}
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		html := renderDetailIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}
