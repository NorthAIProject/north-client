package i18n

// englishCheckins is the daily check-in page.
var englishCheckins = map[string]string{
	"checkins.mono":    "Daily loop",
	"checkins.heading": "Check-in",
	"checkins.intro":   "Thirty seconds. Mood, energy, what went well, what was hard.",
	"checkins.saved":   "Saved for today.",

	"checkins.kpi.streak":    "Streak",
	"checkins.kpi.avgmood":   "Avg mood",
	"checkins.kpi.avgenergy": "Avg energy",
	"checkins.kpi.logged":    "Logged",
	"checkins.kpi.checkcta":  "Check in",
	// The streak and the count both carry a number, so both need a singular.
	"checkins.kpi.streak.one":  "1 day",
	"checkins.kpi.streak.many": "%[1]d days",
	"checkins.kpi.count.one":   "1 in 14d",
	"checkins.kpi.count.many":  "%[1]d in 14d",

	"checkins.mood.title":    "Mood & energy",
	"checkins.mood.desc":     "Trailing fourteen days.",
	"checkins.mood.empty":    "No check-ins yet in this window.",
	"checkins.heatmap.title": "Mood heatmap",
	"checkins.heatmap.desc":  "Days you checked in, coloured by mood.",

	"checkins.recent": "Recent",
	"checkins.edit":   "Edit today's check-in",
	"checkins.back":   "Back to dashboard",

	"checkins.today": "Today",
	"checkins.first": "Your first check-in. Thirty seconds. Khepri will remember this.",
	"checkins.again": "Same-day edits replace the earlier entry. One reflection per day.",

	"checkins.q.mood":   "How is your mood?",
	"checkins.q.energy": "How is your energy?",
	"checkins.q.wins":   "What went well?",
	"checkins.q.hard":   "What was hard?",
	"checkins.q.notes":  "Anything else?",

	"checkins.ph.wins":  "Optional. A small win counts.",
	"checkins.ph.hard":  "Optional.",
	"checkins.ph.notes": "Optional notes for your coach.",

	"checkins.goal":      "Related goal (optional)",
	"checkins.goal.none": "None",

	"checkins.field.mood":   "Mood",
	"checkins.field.energy": "Energy",
	"checkins.step.wins":    "Wins",
	"checkins.step.hard":    "Hard",
	"checkins.step.notes":   "Notes",

	"checkins.nav.back": "Back",
	"checkins.nav.next": "Next",
	"checkins.nav.save": "Save check-in",

	// Screen-reader only. The scale runs 1–5 and the number is not the same
	// word order in every language, so both take explicit indexes.
	"checkins.scale.legend": "%[1]s on a scale of 1 to 5",
	"checkins.scale.option": "%[1]s %[2]d of 5",
	"checkins.scale.low":    "Low",
	"checkins.scale.high":   "High",
}
