package profiles

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func sampleCard() lift.Card {
	end := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	return lift.Card{
		CardSession: lift.CardSession{ID: uuid.New(), Name: "Weight training", EndedAt: end, Duration: time.Hour},
		Sets:        3, VolumeKg: 1200, PRs: 1,
		Exercises: []lift.CardExercise{{Name: "Squat", Sets: 3, VolumeKg: 1200, BestWeightKg: 100, BestReps: 5, PR: true}},
	}
}

func render(t *testing.T, locale users.Locale, shared bool, self bool) string {
	t.Helper()
	n := 4
	d := Data{
		User: users.User{DisplayName: "Ana", Locale: locale},
		Name: "Leo Costa", Handle: "leo", Followers: 2, Following: 3,
		HasLevel: true, LevelNumber: 3, LevelTitle: "Mover",
		Self: self, Workouts: &n, Shared: shared,
	}
	if shared {
		d.Recent = []lift.Card{sampleCard()}
	}
	ctx := i18n.WithLocale(context.Background(), string(locale))
	var b strings.Builder
	if err := ProfilePage(d).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if err := WorkoutPage(d, sampleCard()).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if err := WorkoutsPage(d, []lift.Card{sampleCard()}, "?before=x").Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestProfileRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Workouts", "Followers", "Following", "See all", "Level 3 · Mover", "Follow"},
		users.LocalePTPT: {"Treinos", "Seguidores", "A seguir", "Ver tudo"},
		users.LocalePTBR: {"Treinos", "Seguidores", "Seguindo", "Ver tudo"},
		users.LocaleES:   {"Entrenamientos", "Seguidores", "Siguiendo", "Ver todo"},
	}
	for locale, wants := range cases {
		html := render(t, locale, true, false)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page lacks %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedProfile(t *testing.T) {
	leaks := []string{"Followers", "See all", "Workouts aren", "No workouts yet.", "More", "Level 3", "Unfollow", "Back to"}
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		for _, shared := range []bool{true, false} {
			html := render(t, locale, shared, false)
			for _, leak := range leaks {
				if strings.Contains(html, leak) {
					t.Errorf("%s shared=%v: English %q on the page", locale, shared, leak)
				}
			}
		}
	}
}
