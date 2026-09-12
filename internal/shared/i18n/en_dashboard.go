package i18n

// englishDashboard is /app, the first page after signing in, plus the time
// ranges it shares with the insights pages.
var englishDashboard = map[string]string{
	"dash.mono":           "Command center",
	"dash.hello":          "Hello, %[1]s",
	"dash.intro":          "Everything across Khepri, for %[1]s.",
	"dash.range":          "Time period",
	"dash.notnow":         "Not now",
	"dash.briefing.empty": "No briefing yet this morning.",
	"dash.briefing.write": "Write today's briefing",

	"dash.kpi.checkin":  "Check-in",
	"dash.kpi.streak":   "Streak",
	"dash.kpi.water":    "Water",
	"dash.kpi.sleep":    "Sleep",
	"dash.kpi.burned":   "Burned",
	"dash.kpi.done":     "Done",
	"dash.kpi.pending":  "Pending",
	"dash.kpi.edit":     "Edit",
	"dash.kpi.checkcta": "Check in",
	"dash.kpi.logwater": "Log water",
	"dash.kpi.logsleep": "Log sleep",
	"dash.kpi.activity": "Activity",
	// One day is not "1 days" in any of these languages.
	"dash.kpi.streak.one":  "1 day",
	"dash.kpi.streak.many": "%[1]d days",

	"dash.timeline.title": "Activity",
	"dash.timeline.desc":  "Everything you did, %[1]s.",
	"dash.timeline.empty": "Nothing logged in this window yet.",
	"dash.mood.empty":     "No check-ins yet in this window.",
	"dash.activity.title": "Where it went",
	"dash.activity.desc":  "Your logged activity by kind.",

	"dash.mood.title": "Mood & energy",
	"dash.mood.desc":  "The trailing fortnight from your check-ins.",
	"dash.mood.cta":   "Check in",

	"dash.habits.title":    "Habits",
	"dash.habits.desc":     "Seven-day adherence across active habits.",
	"dash.habits.empty":    "Recurring intentions you can keep or miss.",
	"dash.habits.cta":      "Add a habit",
	"dash.hydration.title": "Hydration",
	"dash.hydration.desc":  "Daily intake in millilitres.",
	"dash.hydration.empty": "Water logged from Care.",
	"dash.hydration.cta":   "Log water",

	"dash.goals.title": "Goals",
	"dash.goals.cta":   "Add a goal",
	"dash.goals.all":   "All goals",

	"dash.reflect": "Reflect",

	"dash.coach.title": "Coach",
	"dash.coach.cont":  "Continue",
	"dash.coach.start": "Start a conversation",

	"dash.training.title":   "Training",
	"dash.training.session": "Open session",
	"dash.training.plan":    "Open plan",
	"dash.training.build":   "Build a training plan",

	// Keyed off timerange.Key. Shared with the insights pages, which use the
	// same selector.
	"range.today":     "Today",
	"range.yesterday": "Yesterday",
	"range.week":      "Last 7 days",
	"range.month":     "Last 30 days",
	"range.quarter":   "Last 90 days",
	"range.year":      "Last 12 months",
}
