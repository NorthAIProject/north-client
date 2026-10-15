package moments_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/moments"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Only the thresholds earn a card. A card on every day is wallpaper.
func TestForStreakFiresOnThresholdsOnly(t *testing.T) {
	t.Parallel()

	for _, n := range []int{3, 7, 14, 30, 60, 100} {
		m, ok := moments.ForStreak(context.Background(), n)
		if !ok {
			t.Errorf("streak %d: no moment", n)
			continue
		}
		if m.Kind != moments.KindStreak || m.Title == "" || m.Body == "" {
			t.Errorf("streak %d: incomplete moment %+v", n, m)
		}
	}
	for _, n := range []int{0, 1, 2, 4, 6, 8, 15, 29, 31, 99, 101} {
		if _, ok := moments.ForStreak(context.Background(), n); ok {
			t.Errorf("streak %d: unexpected moment", n)
		}
	}
}

func TestGoalAndMilestoneCardsNameTheThing(t *testing.T) {
	t.Parallel()

	g := moments.ForGoalAchieved(context.Background(), "Run a half marathon")
	if g.Kind != moments.KindGoal || !strings.Contains(g.Title, "Run a half marathon") {
		t.Fatalf("goal moment = %+v", g)
	}

	m := moments.ForMilestoneCompleted(context.Background(), "Run a half marathon", "First 10k")
	if m.Kind != moments.KindMilestone || !strings.Contains(m.Body, "First 10k") || !strings.Contains(m.Body, "Run a half marathon") {
		t.Fatalf("milestone moment = %+v", m)
	}
}

// Without a locale the cards read as they always have; with one, they read in
// that language and still name the person's own words untouched.
func TestMomentsSpeakTheRequestLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		locale    string
		streak    string
		goal      string
		milestone string
	}{
		{"", "A full week", "“Run a half marathon” — achieved", "“First 10k”, on the way to “Run a half marathon”."},
		{"es", "Una semana entera", "“Run a half marathon”: conseguido", "“First 10k”, de camino a “Run a half marathon”."},
		{"pt-PT", "Uma semana inteira", "“Run a half marathon”: conseguido", "“First 10k”, a caminho de “Run a half marathon”."},
	}
	for _, c := range cases {
		ctx := context.Background()
		if c.locale != "" {
			ctx = i18n.WithLocale(ctx, c.locale)
		}
		if m, _ := moments.ForStreak(ctx, 7); m.Title != c.streak {
			t.Errorf("%q streak 7 title = %q, want %q", c.locale, m.Title, c.streak)
		}
		if g := moments.ForGoalAchieved(ctx, "Run a half marathon"); g.Title != c.goal {
			t.Errorf("%q goal title = %q, want %q", c.locale, g.Title, c.goal)
		}
		if m := moments.ForMilestoneCompleted(ctx, "Run a half marathon", "First 10k"); m.Body != c.milestone {
			t.Errorf("%q milestone body = %q, want %q", c.locale, m.Body, c.milestone)
		}
	}
}
