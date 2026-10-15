package xp

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// What each kind pays for, what a board counts and the level's name read in
// the viewer's language; without one, in English as they always have.
func TestXPLabelsSpeakTheRequestLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		locale string
		got    func(context.Context) string
		want   string
	}{
		{"", func(ctx context.Context) string { return kindLabel(ctx, KindWorkout) }, "Workouts of 10 min or more (up to 2 a day)"},
		{"", func(ctx context.Context) string { return kindLabel(ctx, KindStreakMark) }, "Check-in streaks reaching 7, 30, 100 or 365 days"},
		{"es", func(ctx context.Context) string { return kindLabel(ctx, KindWorkout) }, "Entrenamientos de 10 min o más (hasta 2 al día)"},
		{"es", func(ctx context.Context) string { return kindLabel(ctx, KindStreakMark) }, "Rachas de registros que llegan a 7, 30, 100 o 365 días"},
		{"pt-PT", func(ctx context.Context) string { return kindLabel(ctx, KindStreakDay) }, "Dias de sequência de registos (a partir do dia 3)"},
		{"pt-PT", func(ctx context.Context) string { return kindLabel(ctx, KindHabitKept) }, "Hábitos cumpridos nos seus dias"},
		{"es", func(ctx context.Context) string { return valueText(ctx, MetricStreak, 12) }, "12 días"},
		{"pt-PT", func(ctx context.Context) string { return valueText(ctx, MetricWorkouts, 1) }, "1 treino"},
		{"pt-PT", func(ctx context.Context) string { return switchLabel(ctx, MetricXP) }, "O teu XP e nível"},
		{"", func(ctx context.Context) string { return projectLevel(ctx, LevelFor(6000)).Title }, "Relentless"},
		{"es", func(ctx context.Context) string { return projectLevel(ctx, LevelFor(0)).Title }, i18n.Translate("es", "leaderboard.level.starter")},
	}
	for _, c := range cases {
		ctx := context.Background()
		if c.locale != "" {
			ctx = i18n.WithLocale(ctx, c.locale)
		}
		if got := c.got(ctx); got != c.want {
			t.Errorf("%q: got %q, want %q", c.locale, got, c.want)
		}
	}
}
