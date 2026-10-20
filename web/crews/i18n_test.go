package crews

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderBoardIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	board := crew.Board{
		Crew:      crew.Crew{ID: uuid.New(), Name: "Morning crew", Members: 2},
		Challenge: &crew.Challenge{Kind: crew.ChallengeWorkouts, Target: 3},
		Members: []crew.Member{
			{ID: uuid.New(), DisplayName: "Ana", Me: true, Owner: true, CheckedIn: true, Streak: 4},
			{ID: uuid.New(), DisplayName: "Bruno", Streak: 1},
		},
		IsOwner: true,
		MyRole:  crew.RoleOwner,
	}
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	if err := BoardPage(users.User{DisplayName: "Ana", Locale: locale}, board, "https://khepri.test/i/c/x").Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestCrewBoardRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"3 workouts each, Monday to Sunday.", "Weekly challenge", "Leave crew", "4-day check-in streak"},
		users.LocalePTPT: {"3 treinos cada um, de segunda a domingo.", "Desafio semanal", "Sair do grupo", "Sequência de registos de 4 dias"},
		users.LocalePTBR: {"3 treinos cada um, de segunda a domingo.", "Desafio semanal", "Sair do grupo", "Sequência de registros de 4 dias"},
		users.LocaleES:   {"3 entrenamientos cada uno, de lunes a domingo.", "Reto semanal", "Salir del grupo", "Racha de registros de 4 días"},
	}
	for locale, wants := range cases {
		html := renderBoardIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedCrewBoard(t *testing.T) {
	englishOnly := []string{"Weekly challenge", "Invite to the crew", "Leave crew", "Checked in", "Not yet", "Times a week"}
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		html := renderBoardIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}
