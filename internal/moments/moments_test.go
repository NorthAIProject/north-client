package moments_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
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

// A moment belongs to the save that made today's check-in. Filing it again is
// an edit: the card has been seen.
func TestForCheckInOnlyOnACreatingSave(t *testing.T) {
	t.Parallel()

	run := checkin.Run{Current: 7}
	if _, ok := moments.ForCheckIn(context.Background(), run, false); ok {
		t.Fatal("an edit should not earn a moment")
	}
	if m, ok := moments.ForCheckIn(context.Background(), run, true); !ok || m.Kind != moments.KindStreak {
		t.Fatalf("a creating save on day 7 = %+v, %v; want the streak card", m, ok)
	}
}

// One card per save, the rarest first: a streak mark, then a full week, then a
// rest day earned, then a new run after a long one.
func TestForCheckInPrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  checkin.Run
		want string
	}{
		{"a mark beats everything", checkin.Run{Current: 7, Best: 7, WeekComplete: true, RestEarnedToday: true}, moments.KindStreak},
		{"a full week", checkin.Run{Current: 9, Best: 9, WeekComplete: true, RestEarnedToday: true}, moments.KindPerfectWeek},
		{"a rest earned", checkin.Run{Current: 28, Best: 28, RestEarnedToday: true}, moments.KindRestEarned},
		{"a new run after a long one", checkin.Run{Current: 1, Best: 23, PreviousRun: 23}, moments.KindComeback},
		{"no comeback between single days", checkin.Run{Current: 1, Best: 10, PreviousRun: 1}, ""},
		{"no comeback after a short one", checkin.Run{Current: 1, Best: 2, PreviousRun: 2}, ""},
		{"an ordinary day", checkin.Run{Current: 5, Best: 5}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, ok := moments.ForCheckIn(context.Background(), tc.run, true)
			if tc.want == "" {
				if ok {
					t.Fatalf("unexpected moment %+v", m)
				}
				return
			}
			if !ok || m.Kind != tc.want || m.Title == "" || m.Body == "" {
				t.Fatalf("moment = %+v, %v; want kind %s", m, ok, tc.want)
			}
		})
	}
}

// A new run names the best one and never the lapse: pointing at a broken
// streak is what drives people away.
func TestComebackNamesTheBestRun(t *testing.T) {
	t.Parallel()

	m, _ := moments.ForCheckIn(context.Background(), checkin.Run{Current: 1, Best: 23, PreviousRun: 4}, true)
	if !strings.Contains(m.Body, "23") {
		t.Fatalf("comeback body %q should name the best run", m.Body)
	}
	for _, word := range []string{"broke", "lost", "missed", "failed"} {
		if strings.Contains(strings.ToLower(m.Title+" "+m.Body), word) {
			t.Fatalf("comeback copy %q/%q should not dwell on the lapse (%q)", m.Title, m.Body, word)
		}
	}
}
