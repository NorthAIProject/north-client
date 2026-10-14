package leaderboard

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderLeaderboardIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	p := Page{
		Level:     Level{Number: 2, Title: "Mover", Total: 150, Floor: 100, Next: 300},
		WeekTotal: 40,
		Board:     "xp-week",
		Rows:      []Row{{Rank: 1, Name: "Ana", Value: "40 XP", Level: "Mover", Me: true}},
	}
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	if err := LeaderboardPage(users.User{DisplayName: "Ana", Locale: locale}, p).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestLeaderboardRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Leaderboard", "Level 2", "150 XP · 150 to the next level", "Workouts this week", "Only you so far", "(you)"},
		users.LocalePTPT: {"Classificação", "Nível 2", "150 XP · 150 para o próximo nível", "Em movimento", "Por agora só estás tu", "(tu)"},
		users.LocalePTBR: {"Classificação", "Nível 2", "Treinos nesta semana", "Por enquanto só tem você", "(você)"},
		users.LocaleES:   {"Clasificación", "Nivel 2", "150 XP · 150 para el siguiente nivel", "En marcha", "De momento solo estás tú", "(tú)"},
	}
	for locale, wants := range cases {
		html := renderLeaderboardIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedLeaderboard(t *testing.T) {
	englishOnly := []string{">Leaderboard<", "Level 2", "XP this week", "Only you so far", "What followers see", "Mover"}
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		html := renderLeaderboardIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}
