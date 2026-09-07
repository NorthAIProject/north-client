package checkins

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderCheckinsIn(t *testing.T, locale users.Locale, streak int, inst Instruments) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	user := users.User{DisplayName: "Ana", Locale: locale}
	page := IndexPage(user, nil, CheckInForm{}, nil, streak, false, nil, inst)
	if err := page.Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestCheckinsRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Daily loop", "How is your mood?", "Save check-in"},
		users.LocalePTPT: {"Ciclo diário", "Como está o teu humor?", "Guardar registo"},
		users.LocalePTBR: {"Ciclo diário", "Como está seu humor?", "Salvar registro"},
		users.LocaleES:   {"Ciclo diario", "¿Cómo está tu ánimo?", "Guardar registro"},
	}
	for locale, wants := range cases {
		html := renderCheckinsIn(t, locale, 0, Instruments{})
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// Both counters on this page carry a number, and both have a one case.
func TestCheckinsSingularsAreCorrect(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"1 day", "1 in 14d"},
		users.LocalePTPT: {"1 dia", "1 em 14 dias"},
		users.LocaleES:   {"1 día", "1 en 14 días"},
	}
	for locale, wants := range cases {
		html := renderCheckinsIn(t, locale, 1, Instruments{CheckInCount: 1, HasData: true})
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: expected the singular %q", locale, want)
			}
		}
	}
}

// The 1–5 scale is announced to screen readers with the field name
// interpolated. Word order around it differs by language, so both strings use
// explicit argument indexes.
func TestTheScaleIsAnnouncedInTheChosenLanguage(t *testing.T) {
	html := renderCheckinsIn(t, users.LocaleES, 0, Instruments{})

	for _, want := range []string{"Ánimo en una escala de 1 a 5", "Ánimo 1 de 5"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected the screen-reader text %q", want)
		}
	}
}
