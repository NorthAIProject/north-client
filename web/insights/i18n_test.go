package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderIn(t *testing.T, locale string, c templ.Component) string {
	t.Helper()
	ctx := context.Background()
	if locale != "" {
		ctx = i18n.WithLocale(ctx, locale)
	}
	var b strings.Builder
	if err := c.Render(ctx, &b); err != nil {
		t.Fatalf("render %q: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%q rendered nothing", locale)
	}
	return b.String()
}

func assertPage(t *testing.T, html, locale string, wants, leaks []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("%s: page does not contain %q", locale, want)
		}
	}
	for _, leak := range leaks {
		if strings.Contains(html, leak) {
			t.Errorf("%s: English %q is still on the page", locale, leak)
		}
	}
}

// The shell every insights page shares: the sub-nav, the heading and the
// labels a screen reader announces.
func TestInsightsShellRendersInTheChosenLanguage(t *testing.T) {
	user := users.User{DisplayName: "Ana", Locale: users.LocaleES}
	page := Progress(user, ProgressView{Range: RangeView{Key: "week", Label: "Últimos 7 días"}})

	html := renderIn(t, "es", page)
	assertPage(t, html, "es",
		[]string{"Resumen", "Cuerpo", "Patrones", "Gasto", "Cómo te ha ido — Últimos 7 días.", "Secciones de análisis", "Objetivos activos", "Cómo va cada objetivo"},
		[]string{"Insights sections", "Time period", "How it has been going", ">Patterns<", "Active goals", "Where each goal stands"},
	)
}

// Every section's panels, empty: the empty states are what a new account sees
// first, so they are the strings most likely to be read in the wrong language.
func TestInsightsPanelsRenderInTheChosenLanguage(t *testing.T) {
	cases := []struct {
		name   string
		c      templ.Component
		wants  map[string][]string
		leaked []string
	}{
		{
			name: "body",
			c:    BodyPanels(BodyView{}),
			wants: map[string][]string{
				"es":    {"Hidratación", "Aún no hay hábitos.", "ninguna noche valorada"},
				"pt-PT": {"Hidratação", "Ainda não há hábitos.", "Registar água"},
				"pt-BR": {"Hidratação", "Ainda não há hábitos.", "Registrar água"},
			},
			leaked: []string{"Hydration", "No habits yet.", "no nights rated", "Log water"},
		},
		{
			name: "training",
			c:    TrainingPanels(TrainingView{}),
			wants: map[string][]string{
				"es":    {"Energía quemada", "Pesas", "No hay sesiones en este periodo."},
				"pt-PT": {"Energia gasta", "Musculação", "Não há sessões neste período."},
				"pt-BR": {"Energia gasta", "Musculação", "Não há sessões neste período."},
			},
			leaked: []string{"Energy burned", ">Lifting<", "No sessions in this window."},
		},
		{
			name: "summary",
			c:    SummaryPanels(SummaryView{Empty: true}),
			wants: map[string][]string{
				"es":    {"Aún no hay nada registrado en este periodo.", "Ir a hoy"},
				"pt-PT": {"Ainda não há nada registado neste período.", "Ir para hoje"},
				"pt-BR": {"Ainda não há nada registrado neste período.", "Ir para hoje"},
			},
			leaked: []string{"Nothing logged in this window yet.", "Go to today"},
		},
		{
			name: "patterns",
			c:    PatternsPanels(PatternsView{Days: 30}),
			wants: map[string][]string{
				"es":    {"en los últimos 30 días", "Aún no destaca nada."},
				"pt-PT": {"nos últimos 30 dias", "Ainda nada se destaca."},
				"pt-BR": {"nos últimos 30 dias", "Ainda nada se destaca."},
			},
			leaked: []string{"over the last 30 days", "Nothing stands out yet."},
		},
	}

	for _, tc := range cases {
		for locale, wants := range tc.wants {
			html := renderIn(t, locale, tc.c)
			assertPage(t, html, locale+"/"+tc.name, wants, tc.leaked)
		}
	}
}

// Without a locale — a test, a background render — the page reads English,
// exactly as it did before it was translated.
func TestInsightsPanelsDefaultToEnglish(t *testing.T) {
	html := renderIn(t, "", BodyPanels(BodyView{}))
	assertPage(t, html, "en", []string{"Hydration", "No habits yet.", "no nights rated"}, nil)
}

// A date older than a week prints its month in the reader's language.
func TestRelativeTimeNamesTheMonthInTheChosenLanguage(t *testing.T) {
	at := time.Date(2020, time.January, 5, 9, 0, 0, 0, time.UTC)
	es := i18n.WithLocale(context.Background(), "es")
	if got := relativeTime(es, at); got != "5 ene" {
		t.Errorf("es: relativeTime = %q, want %q", got, "5 ene")
	}
	if got := relativeTime(context.Background(), at); got != "5 Jan" {
		t.Errorf("en: relativeTime = %q, want %q", got, "5 Jan")
	}
}
