package moments_test

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/moments"
)

// Only the thresholds earn a card. A card on every day is wallpaper.
func TestForStreakFiresOnThresholdsOnly(t *testing.T) {
	t.Parallel()

	for _, n := range []int{3, 7, 14, 30, 60, 100} {
		m, ok := moments.ForStreak(n)
		if !ok {
			t.Errorf("streak %d: no moment", n)
			continue
		}
		if m.Kind != moments.KindStreak || m.Title == "" || m.Body == "" {
			t.Errorf("streak %d: incomplete moment %+v", n, m)
		}
	}
	for _, n := range []int{0, 1, 2, 4, 6, 8, 15, 29, 31, 99, 101} {
		if _, ok := moments.ForStreak(n); ok {
			t.Errorf("streak %d: unexpected moment", n)
		}
	}
}

func TestGoalAndMilestoneCardsNameTheThing(t *testing.T) {
	t.Parallel()

	g := moments.ForGoalAchieved("Run a half marathon")
	if g.Kind != moments.KindGoal || !strings.Contains(g.Title, "Run a half marathon") {
		t.Fatalf("goal moment = %+v", g)
	}

	m := moments.ForMilestoneCompleted("Run a half marathon", "First 10k")
	if m.Kind != moments.KindMilestone || !strings.Contains(m.Body, "First 10k") || !strings.Contains(m.Body, "Run a half marathon") {
		t.Fatalf("milestone moment = %+v", m)
	}
}
