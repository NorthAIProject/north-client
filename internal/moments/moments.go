// Package moments decides when something a person did deserves a beat of
// recognition, and what to say.
//
// It exists because North has had the accounting for progress — check-in
// streaks, habit streaks, goal progress, milestones — for months, and never
// once closed the loop: saving the seventh check-in in a row said "Same time
// tomorrow." exactly as the first one did, and marking a goal achieved was a
// redirect. The mascot has had a celebrate gesture since it was built and
// used it only on finishing onboarding.
//
// This is not a points system, and should not grow into one here. A moment is
// recognition of an outcome the coach can verify — a run of check-ins, a goal
// closed, a checkpoint reached — with nothing to accumulate and nothing to
// spend. See docs/advanced-gamification.md for what an economy would look like
// and the numbers that would justify it.
//
// Pure functions, no storage. Whether a moment is *shown* is the handler's
// business; whether one is *earned* is decided here so the thresholds and the
// words live in one place.
package moments

import (
	"context"
	"fmt"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Kinds, as they reach the funnel (analytics.EventMomentShown) and the app.
// Open-ended on the wire: clients must tolerate kinds they do not know.
const (
	KindStreak      = "streak"
	KindGoal        = "goal_achieved"
	KindMilestone   = "milestone_completed"
	KindFirstWeek   = "first_week_three"
	KindPerfectWeek = "perfect_week"
	KindRestEarned  = "rest_day_earned"
	KindComeback    = "comeback"
)

// comebackAfter is the shortest ended run a new run is worth marking after.
// Below it, every other day would be a "fresh start", and "your best is 2
// days" is not recognition.
const comebackAfter = 3

// Moment is one card: the mascot, a title, a line.
type Moment struct {
	Kind  string
	Title string
	Body  string
}

// streakMarks are the check-in streaks that earn a card, and nothing in
// between. Sparse on purpose: a card every day is wallpaper by day four. The
// words are in the moments catalogue, under moments.streak.<n>.
var streakMarks = map[int]bool{3: true, 7: true, 14: true, 30: true, 60: true, 100: true}

// ForStreak reports whether a check-in streak of n days is worth a card, and
// which, in the request's language. Only the exact thresholds qualify: day 8
// is not a moment, day 7 was.
func ForStreak(ctx context.Context, n int) (Moment, bool) {
	if !streakMarks[n] {
		return Moment{}, false
	}
	key := fmt.Sprintf("moments.streak.%d", n)
	return Moment{Kind: KindStreak, Title: i18n.T(ctx, key+".title"), Body: i18n.T(ctx, key+".body")}, true
}

// ForCheckIn is the one card a check-in save earns, if any. Only the save
// that created today's check-in qualifies: filing it again is an edit, and
// the card has been seen.
//
// One card per save, the rarest first: a streak mark, then a full Monday to
// Sunday week, then a rest day earned, then the first day of a new run after
// a long one. The last names the best run, never the gap before it.
func ForCheckIn(ctx context.Context, run checkin.Run, created bool) (Moment, bool) {
	if !created {
		return Moment{}, false
	}
	if m, ok := ForStreak(ctx, run.Current); ok {
		return m, true
	}
	switch {
	case run.WeekComplete:
		return Moment{
			Kind:  KindPerfectWeek,
			Title: i18n.T(ctx, "moments.perfect_week.title"),
			Body:  i18n.T(ctx, "moments.perfect_week.body"),
		}, true
	case run.RestEarnedToday:
		return Moment{
			Kind:  KindRestEarned,
			Title: i18n.T(ctx, "moments.rest_earned.title"),
			Body:  i18n.T(ctx, "moments.rest_earned.body"),
		}, true
	case run.Current == 1 && run.PreviousRun >= comebackAfter:
		return Moment{
			Kind:  KindComeback,
			Title: i18n.T(ctx, "moments.comeback.title"),
			Body:  i18n.Tf(ctx, "moments.comeback.body", run.Best),
		}, true
	}
	return Moment{}, false
}

// ForGoalAchieved is the card for a goal moved to achieved.
func ForGoalAchieved(ctx context.Context, title string) Moment {
	return Moment{
		Kind:  KindGoal,
		Title: i18n.Tf(ctx, "moments.goal.title", title),
		Body:  i18n.T(ctx, "moments.goal.body"),
	}
}

// ForMilestoneCompleted is the card for a checkpoint on the way to a goal.
func ForMilestoneCompleted(ctx context.Context, goalTitle, milestoneTitle string) Moment {
	return Moment{
		Kind:  KindMilestone,
		Title: i18n.T(ctx, "moments.milestone.title"),
		Body:  i18n.Tf(ctx, "moments.milestone.body", milestoneTitle, goalTitle),
	}
}

// ForFirstWeekThree is the card for the first three days of a first week
// (internal/firstweek) all done: the point where one action a day starts to
// look like a habit.
func ForFirstWeekThree(ctx context.Context) Moment {
	return Moment{
		Kind:  KindFirstWeek,
		Title: i18n.T(ctx, "moments.firstweek.three.title"),
		Body:  i18n.T(ctx, "moments.firstweek.three.body"),
	}
}
