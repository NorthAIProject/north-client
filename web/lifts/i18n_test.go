package lifts

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderIn(t *testing.T, locale users.Locale, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(i18n.WithLocale(context.Background(), string(locale)), &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

func today() Today {
	return Today{
		Sets: []SetRow{{ID: "1", Exercise: "Bench Press", Load: "80 kg × 5", Warmup: true}},
		Form: SetForm{Exercise: "Bench Press"},
	}
}

func TestSetLoggerRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Log a set", "Warm-up", "Log set", "Remove Bench Press 80 kg × 5"},
		users.LocaleES:   {"Registrar una serie", "Calentamiento", "Registrar serie", "Quitar Bench Press 80 kg × 5"},
		users.LocalePTPT: {"Registar uma série", "Aquecimento", "Registar série", "Remover Bench Press 80 kg × 5"},
		users.LocalePTBR: {"Registrar uma série", "Aquecimento", "Registrar série", "Remover Bench Press 80 kg × 5"},
	}
	for locale, wants := range cases {
		html := renderIn(t, locale, TodayCard(today()))
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: set logger does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedSetLogger(t *testing.T) {
	for _, locale := range []users.Locale{users.LocaleES, users.LocalePTPT, users.LocalePTBR} {
		html := renderIn(t, locale, TodayCard(today()))
		for _, leak := range []string{"Log a set", "Warm-up", "In reserve", "Kind of set", "Today's sets"} {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the set logger", locale, leak)
			}
		}
	}
}

func TestImportPageAndResultAreTranslated(t *testing.T) {
	result := &Imported{Workouts: 12, Sets: 340, Duplicates: 2}
	html := renderIn(t, users.LocaleES, ImportPage(users.User{DisplayName: "Ana", Locale: users.LocaleES}, result, ""))
	for _, want := range []string{"Trae tu historial de Hevy", "Importados 12 entrenos, 340 series", "2 entrenos ya estaban aquí", "Importando tus entrenos…"} {
		if !strings.Contains(html, want) {
			t.Errorf("import page does not contain %q", want)
		}
	}
	for _, leak := range []string{"Bring your Hevy history", "Imported 12 workouts", "Back to training"} {
		if strings.Contains(html, leak) {
			t.Errorf("English %q is still on the import page", leak)
		}
	}
}
