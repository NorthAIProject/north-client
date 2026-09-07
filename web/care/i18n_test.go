package care

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderCareIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	user := users.User{DisplayName: "Ana", Locale: locale}
	if err := Page(user, Data{}, Forms{}).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestCareRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Care", "Add habit", "Nothing due right now."},
		users.LocalePTPT: {"Cuidado", "Adicionar hábito", "Nada a cumprir neste momento."},
		users.LocalePTBR: {"Cuidado", "Adicionar hábito", "Nada para agora."},
		users.LocaleES:   {"Cuidado", "Añadir hábito", "Nada pendiente ahora mismo."},
	}
	for locale, wants := range cases {
		html := renderCareIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// Seven weekday checkboxes sit in one row on a phone, so they are
// abbreviations. Spanish and Portuguese abbreviate differently — Lun against
// Seg — which is exactly the kind of thing a shared "pt/es are close enough"
// shortcut would have got wrong.
func TestWeekdayAbbreviationsAreTranslated(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Sun", "Mon", "Sat"},
		users.LocalePTPT: {"Dom", "Seg", "Sáb"},
		users.LocaleES:   {"Dom", "Lun", "Sáb"},
	}
	for locale, wants := range cases {
		html := renderCareIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, ">"+want+"<") {
				t.Errorf("%s: weekday %q is missing", locale, want)
			}
		}
	}
}

// The habit "Area" select renders the same lifedomain vocabulary the goals
// page uses as a category. It used to capitalise the raw identifier, which
// produced English regardless of the setting.
func TestHabitAreaUsesTheSharedVocabulary(t *testing.T) {
	html := renderCareIn(t, users.LocaleES)

	for _, want := range []string{"Salud", "Trabajo", "Aprendizaje"} {
		if !strings.Contains(html, want) {
			t.Errorf("Spanish life domain %q is missing from the area select", want)
		}
	}
	if strings.Contains(html, ">Health<") {
		t.Error("the area select still capitalises the raw identifier")
	}
}

// Every offered weekday index must resolve, and anything outside 0–6 must
// render nothing rather than a raw key.
func TestWeekdayLabelBounds(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), string(users.LocalePTBR))

	for d := 0; d <= 6; d++ {
		if got := weekdayLabel(ctx, d); got == "" || strings.HasPrefix(got, "weekday.") {
			t.Errorf("weekday %d renders as %q", d, got)
		}
	}
	for _, d := range []int{-1, 7, 99} {
		if got := weekdayLabel(ctx, d); got != "" {
			t.Errorf("weekday %d renders as %q, want empty", d, got)
		}
	}
}
