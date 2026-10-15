package i18n

// englishXP is what the XP card and leaderboard say each kind pays for, and how a board counts.
var englishXP = map[string]string{
	// Minutes a workout must last, then how many a day pay.
	"xp.kind.workout":    "Workouts of %[1]d min or more (up to %[2]d a day)",
	"xp.kind.habit_kept": "Habits kept on their days",
	"xp.kind.streak_day": "Check-in streak days (from day %[1]d)",
	"xp.kind.milestone":  "Milestones reached",
	"xp.kind.goal":       "Goals achieved",
	// The marks as a list, "7, 30, 100 or 365", built with xp.marks.or.
	"xp.kind.streak_mark":   "Check-in streaks reaching %[1]s days",
	"xp.kind.week_reviewed": "Weekly reviews done",
	"xp.kind.challenge_met": "Crew challenges met",
	"xp.marks.or":           "%[1]s or %[2]s",
	// The Friends page switches a board needs. Must match the Friends page wording.
	"xp.switch.xp":          "Your XP and level",
	"xp.switch.streak":      "Check-in streaks",
	"xp.switch.workouts":    "Workouts you finish",
	"xp.value.day.one":      "1 day",
	"xp.value.day.many":     "%[1]d days",
	"xp.value.workout.one":  "1 workout",
	"xp.value.workout.many": "%[1]d workouts",
	"xp.err.summary":        "Your XP could not be loaded.",
	"xp.err.board":          "The leaderboard could not be loaded.",
	"xp.err.failed":         "Something went wrong.",
}
