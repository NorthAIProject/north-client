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
		{"", func(ctx context.Context) string { return ProjectLevel(ctx, LevelFor(6000)).Title }, "Relentless"},
		{"es", func(ctx context.Context) string { return ProjectLevel(ctx, LevelFor(0)).Title }, i18n.Translate("es", "leaderboard.level.starter")},
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
