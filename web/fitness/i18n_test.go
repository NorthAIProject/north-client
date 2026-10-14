package fitness

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderHubIn(t *testing.T, locale string) string {
	t.Helper()

	now := time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	data := HubData{
		StravaStatus: strava.Status{Configured: true},
		Week: WeekView{Bars: []WeekBar{
			{Label: "Mo"}, {Label: "Tu"}, {Label: "We", Minutes: 30, IsToday: true},
		}},
		Weight:   &Weight{Kg: 80, At: now},
		Location: time.UTC,
		Now:      now,
	}
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), locale)
	if err := Hub(users.User{DisplayName: "Ana"}, data).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

// The hub is the entrance to every Body page; a Spanish reader should not meet
// it in English.
func TestHubRendersInTheChosenLanguage(t *testing.T) {
	cases := map[string]struct {
		want   []string
		absent []string
	}{
		"en":    {want: []string{"This week", "No sessions yet", "Import your runs and rides automatically.", "Up next"}},
		"es":    {want: []string{"Esta semana", "Aún sin sesiones", "Importa tus carreras", "Lo siguiente", "4 mar", "Mié"}, absent: []string{"This week", "No sessions yet", "Up next", "Log weight"}},
		"pt-PT": {want: []string{"Esta semana", "Ainda sem sessões", "Registar peso", "A seguir", "Qua"}, absent: []string{"This week", "Log weight"}},
		"pt-BR": {want: []string{"Nenhuma sessão ainda", "Registrar peso", "Conectar o Strava"}, absent: []string{"No sessions yet", "Connect Strava"}},
	}

	for locale, tc := range cases {
		html := renderHubIn(t, locale)
		for _, want := range tc.want {
			if !strings.Contains(html, want) {
				t.Errorf("%s: hub does not contain %q", locale, want)
			}
		}
		for _, english := range tc.absent {
			if strings.Contains(html, english) {
				t.Errorf("%s: hub still contains the English %q", locale, english)
			}
		}
	}
}

func TestTrendSentenceTranslates(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), "es")
	got := trendSentence(ctx, strava.Trend{Shape: strava.ShapeBuilding, RecentMinutes: 400, Ratio: 1.33})
	if want := "6h 40m esta semana — un tercio por encima de lo habitual."; got != want {
		t.Errorf("sentence = %q, want %q", got, want)
	}
}

func TestActivitiesEmptyStateTranslates(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "pt-PT")
	page := ActivitiesPage(users.User{}, strava.Status{Configured: true}, strava.Trend{}, strava.SessionPage{})
	if err := page.Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if !strings.Contains(html, "Liga o Strava") || strings.Contains(html, "Not connected") {
		t.Errorf("disconnected state not in pt-PT:\n%s", html)
	}
}
