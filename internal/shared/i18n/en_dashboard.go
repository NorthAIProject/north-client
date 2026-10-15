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

	"dash.kpi.streak":   "Streak",
	"dash.kpi.water":    "Water",
	"dash.kpi.sleep":    "Sleep",
	"dash.kpi.burned":   "Burned",
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

	"dash.briefing.title":       "Briefing",
	"dash.today.title":          "Today",
	"dash.today.progress":       "%[1]d of %[2]d done",
	"dash.today.next":           "Next:",
	"dash.today.alldone":        "That is today done.",
	"dash.today.alldone.body":   "A two-minute reflection is the best thing left to do.",
	"dash.today.checkin":        "Check in",
	"dash.today.checkin.detail": "Mood and energy, thirty seconds.",
	"dash.today.streak":         "Streak: %[1]s",
	"dash.today.train":          "Train: %[1]s",
	"dash.today.train.count":    "%[1]d exercises",
	"dash.today.water":          "Drink water",
	"dash.today.sleep":          "Log last night",
	"dash.today.sleep.none":     "Not logged yet.",
	"dash.training.empty":       "A plan the coach can follow, not a paragraph of sets.",
	"dash.training.library":     "Browse exercises",
	"dash.goals.empty":          "The coach works better once it knows what you are aiming at.",
	"dash.coach.empty":          "Khepri already has your goals and check-ins when they exist.",
	"dash.habits.none":          "No habits yet.",
	"dash.hydration.none":       "No water logged in this window.",

	// Keyed off timerange.Key. Shared with the insights pages, which use the
	// same selector.
	"range.today":     "Today",
	"range.yesterday": "Yesterday",
	"range.week":      "Last 7 days",
	"range.month":     "Last 30 days",
	"range.quarter":   "Last 90 days",
	"range.year":      "Last 12 months",

	// The check-in row once done: today's numbers and when.
	"dash.today.checkin.done": "Mood %[1]d · energy %[2]d · %[3]s",

	// Feed rows: how long ago, kept short. Older than a week is a date.
	"dash.feed.now":     "just now",
	"dash.feed.minutes": "%[1]dm ago",
	"dash.feed.hours":   "%[1]dh ago",
	"dash.feed.days":    "%[1]dd ago",

	"dash.goals.week.one":  "1 this week",
	"dash.goals.week.many": "%[1]d this week",
	"dash.habits.kept":     "%[1]d kept of %[2]d scheduled",
	"dash.habits.best":     "best streak %[1]d",
	"dash.hydration.today": "Today %[1]d%% · %[2]s",

	// The next-step card, by kind. The native app shows these verbatim.
	"dash.step.goal.eyebrow":    "Start here",
	"dash.step.goal.title":      "Name one thing you are working toward",
	"dash.step.goal.body":       "Khepri reads your goals before every reply. One is enough.",
	"dash.step.goal.cta":        "Add a goal",
	"dash.step.checkin.eyebrow": "Today",
	"dash.step.checkin.title":   "How did today go?",
	"dash.step.checkin.body":    "Thirty seconds. Mood, energy, what went well.",
	"dash.step.checkin.cta":     "Check in",
	"dash.step.chat.eyebrow":    "Coach",
	"dash.step.chat.title":      "Talk to Khepri about the week",
	"dash.step.chat.body":       "It already has your goals. Start with what is in the way.",
	"dash.step.chat.cta":        "Open the coach",
	"dash.step.push.eyebrow":    "Stay on track",
	"dash.step.push.title":      "Let Khepri reach you",
	"dash.step.push.body":       "A short note on this device when you have not checked in, a goal is due, or it is a training day.",
	"dash.step.push.cta":        "Turn on nudges",

	// Chart series names.
	"dash.mood.series":      "Mood",
	"dash.hydration.series": "Water (ml)",

	// Feed row titles and details. Names and amounts arrive formatted.
	"dash.feed.checkin":        "Checked in",
	"dash.feed.checkin.detail": "Mood %[1]d · Energy %[2]d",
	"dash.feed.drank":          "Drank %[1]s",
	"dash.feed.slept":          "Slept %[1]s",
	"dash.feed.sleep.quality":  "Quality %[1]d/5",
	"dash.feed.habit":          "Kept %[1]s",
	"dash.feed.habit.unnamed":  "Kept a habit",
	"dash.feed.journal":        "Wrote in the journal",
	"dash.feed.goalnote":       "Progress on %[1]s",
	"dash.feed.goal":           "Set a goal: %[1]s",
	"dash.feed.caffeine":       "%[1]d mg caffeine",
	"dash.feed.fast.start":     "Started fasting",
	"dash.feed.fast.end":       "Broke the fast",
}
