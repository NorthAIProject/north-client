package checkins

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderCheckinsIn(t *testing.T, locale users.Locale, streak int, inst Instruments) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	user := users.User{DisplayName: "Ana", Locale: locale}
	page := IndexPage(user, CheckInForm{}, nil, false, nil, LiveData{Streak: streak, Instruments: inst})
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

// The receipt after a save: the day, the streak line and the label all follow
// the locale, including the weekday and month names.
func TestSavedPanelIsTranslated(t *testing.T) {
	c := checkin.CheckIn{
		LocalDate:        time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC),
		Mood:             4,
		Energy:           3,
		RelatedGoalTitle: "Run 10k",
	}
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Logged", "Mon 2 Mar — mood 4 · energy 3", "Related: Run 10k", "3-day streak"},
		users.LocalePTPT: {"Registado", "humor 4 · energia 3", "Relacionado: Run 10k", "Sequência de 3 dias"},
		users.LocalePTBR: {"Registrado", "humor 4 · energia 3", "Sequência de 3 dias"},
		users.LocaleES:   {"Registrado", "ánimo 4 · energía 3", "Racha de 3 días", "mar"},
	}
	for locale, wants := range cases {
		var b strings.Builder
		ctx := i18n.WithLocale(context.Background(), string(locale))
		if err := SavedPanel(c, 3, nil).Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		html := b.String()
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: saved panel does not contain %q", locale, want)
			}
		}
		if locale != users.LocaleEN && strings.Contains(html, "Same time tomorrow") {
			t.Errorf("%s: English streak line leaked", locale)
		}
	}
}
