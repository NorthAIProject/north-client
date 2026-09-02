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

import "fmt"

// Kinds, as they reach the funnel (analytics.EventMomentShown).
const (
	KindStreak    = "streak"
	KindGoal      = "goal_achieved"
	KindMilestone = "milestone_completed"
)

// Moment is one card: the mascot, a title, a line.
type Moment struct {
	Kind  string
	Title string
	Body  string
}

// streakCopy is what a check-in streak earns at each threshold, and nothing in
// between. Sparse on purpose: a card every day is wallpaper by day four.
var streakCopy = map[int]Moment{
	3:   {Kind: KindStreak, Title: "Three days running", Body: "That is a habit starting. Same time tomorrow."},
	7:   {Kind: KindStreak, Title: "A full week", Body: "Seven check-ins in a row. Khepri has a real picture of you now."},
	14:  {Kind: KindStreak, Title: "Two weeks", Body: "The reviews get sharper from here. Keep feeding them."},
	30:  {Kind: KindStreak, Title: "Thirty days", Body: "A month of showing up. Most people never get here."},
	60:  {Kind: KindStreak, Title: "Sixty days", Body: "Two months. This is who you are now, not something you are trying."},
	100: {Kind: KindStreak, Title: "One hundred days", Body: "A hundred check-ins. Khepri remembers every one."},
}

// ForStreak reports whether a check-in streak of n days is worth a card, and
// which. Only the exact thresholds qualify: day 8 is not a moment, day 7 was.
func ForStreak(n int) (Moment, bool) {
	m, ok := streakCopy[n]
	return m, ok
}

// ForGoalAchieved is the card for a goal moved to achieved.
func ForGoalAchieved(title string) Moment {
	return Moment{
		Kind:  KindGoal,
		Title: fmt.Sprintf("“%s” — achieved", title),
		Body:  "You named it, you did it. Khepri will remember this one.",
	}
}

// ForMilestoneCompleted is the card for a checkpoint on the way to a goal.
func ForMilestoneCompleted(goalTitle, milestoneTitle string) Moment {
	return Moment{
		Kind:  KindMilestone,
		Title: "Checkpoint reached",
		Body:  fmt.Sprintf("“%s”, on the way to “%s”.", milestoneTitle, goalTitle),
	}
}
