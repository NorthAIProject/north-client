package activity

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderActivityIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	page := Page(users.User{DisplayName: "Ana", Locale: locale}, activity.Session{}, false, nil, 320, "")
	if err := page.Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestActivityRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Start a session", "Log a finished session", "~320 kcal burned today", "Walking (brisk pace)"},
		users.LocalePTPT: {"Começar uma sessão", "Registar uma sessão terminada", "~320 kcal gastas hoje", "Saltar à corda"},
		users.LocalePTBR: {"Começar uma sessão", "Registrar uma sessão concluída", "~320 kcal gastas hoje", "Pular corda"},
		users.LocaleES:   {"Empezar una sesión", "Registrar una sesión terminada", "~320 kcal quemadas hoy", "Saltar a la comba"},
	}
	for locale, wants := range cases {
		html := renderActivityIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// The select lists the whole MET table, so an entry added there without a
// catalogue line would show English in the middle of a translated list.
func TestEveryActivityHasATranslatedName(t *testing.T) {
	// Spelled the same in every locale, so they read from the table itself.
	international := map[string]bool{"yoga": true, "pilates": true, "kickboxing": true}
	for _, locale := range users.Locales {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		for _, m := range activity.METTable {
			if international[m.Code] {
				continue
			}
			key := "activity.met." + m.Code
			if got := i18n.T(ctx, key); got == key {
				t.Errorf("%s: %s has no catalogue entry", locale, m.Code)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedActivityPage(t *testing.T) {
	englishOnly := []string{"Choose one", "Start a session", "Log session", "Distance (km, optional)", "Walking (brisk pace)"}
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		html := renderActivityIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}

// A finished session leads with its date, which names the month.
func TestSessionSummaryNamesTheMonthInTheReadersLanguage(t *testing.T) {
	s := activity.Session{StartedAt: time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)}
	ctx := i18n.WithLocale(context.Background(), string(users.LocaleES))
	if got := sessionSummary(ctx, s); !strings.HasPrefix(got, "5 ene") {
		t.Errorf("es summary = %q, want it to start with \"5 ene\"", got)
	}
}
