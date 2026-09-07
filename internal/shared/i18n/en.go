package i18n

// english is the reference catalogue. Every other locale is checked against its
// key set by TestEveryCatalogueCoversEnglish, so a string added here without a
// translation fails the build rather than shipping in the wrong language.
//
// Keys are grouped by surface and kept in the order the surface renders them,
// which makes a missing one visible by reading rather than by searching.
var english = map[string]string{
	// Sidebar group headings. The Group* constants stay identifiers; these are
	// what a person reads.
	"nav.group.today":    "Today",
	"nav.group.body":     "Body",
	"nav.group.mind":     "Mind",
	"nav.group.progress": "Progress",
	"nav.group.system":   "System",

	"nav.overview":           "Overview",
	"nav.overview.desc":      "Where today stands.",
	"nav.quick-capture":      "Quick capture",
	"nav.quick-capture.desc": "Write your day in one line.",
	"nav.check-ins":          "Check-ins",
	"nav.check-ins.desc":     "How you are doing, in your words.",
	"nav.coach":              "Coach",
	"nav.coach.desc":         "Talk it through.",

	"nav.fitness":                "Fitness",
	"nav.fitness.desc":           "Training, activity, and nutrition in one place.",
	"nav.training":               "Training",
	"nav.training.desc":          "AI-generated workout plans.",
	"nav.new-training-plan":      "New training plan",
	"nav.new-training-plan.desc": "Generate a plan from your goals.",
	"nav.training-plans":         "Training plans",
	"nav.training-plans.desc":    "Every plan you have saved.",
	"nav.exercises":              "Exercises",
	"nav.exercises.desc":         "What each movement trains, on the model.",
	"nav.form-check":             "Form check",
	"nav.form-check.desc":        "Upload a clip, get feedback.",
	"nav.activity-timer":         "Activity timer",
	"nav.activity-timer.desc":    "Track a session, see calories burned.",
	"nav.calculator":             "Calculator",
	"nav.calculator.desc":        "BMR, TDEE, and a macro target.",
	"nav.ingredients":            "Ingredients",
	"nav.ingredients.desc":       "Shared and your own foods.",
	"nav.meal-plans":             "Meal plans",
	"nav.meal-plans.desc":        "Build plans, track totals.",
	"nav.food-log":               "Food log",
	"nav.food-log.desc":          "Log today, see progress.",
	"nav.strava-activities":      "Strava activities",
	"nav.strava-activities.desc": "Your imported runs and rides, in 3D.",
	"nav.care":                   "Care",
	"nav.care.desc":              "Water, sleep, and habits.",

	"nav.mind":           "Mind",
	"nav.mind.desc":      "Journal and reflect.",
	"nav.memory":         "Memory",
	"nav.memory.desc":    "What North remembers about you.",
	"nav.decisions":      "Decisions",
	"nav.decisions.desc": "The choices you made, and why.",

	"nav.goals":                  "Goals",
	"nav.goals.desc":             "What you are working towards.",
	"nav.reports":                "Reports",
	"nav.reports.desc":           "Weekly reviews and daily briefings.",
	"nav.insights":               "Insights",
	"nav.insights.desc":          "Your activity over time.",
	"nav.insights.self":          "Activity",
	"nav.body-insights":          "Body insights",
	"nav.body-insights.desc":     "Weight, measurements, and training load.",
	"nav.body-insights.nav":      "Body",
	"nav.mind-insights":          "Mind insights",
	"nav.mind-insights.desc":     "Mood and reflection over time.",
	"nav.mind-insights.nav":      "Mind",
	"nav.progress-insights":      "Progress insights",
	"nav.progress-insights.desc": "How your goals are moving.",
	"nav.progress-insights.nav":  "Progress",
	"nav.training-insights":      "Training insights",
	"nav.training-insights.desc": "Volume, frequency, and adherence.",
	"nav.training-insights.nav":  "Training",

	"nav.knowledge":              "Knowledge",
	"nav.knowledge.desc":         "Documents and notes North can draw on.",
	"nav.search-knowledge":       "Search knowledge",
	"nav.search-knowledge.desc":  "Find a passage across everything you have added.",
	"nav.settings":               "Settings",
	"nav.settings.desc":          "Profile, preferences, and notifications.",
	"nav.agent-connections":      "Agent connections",
	"nav.agent-connections.desc": "Tokens for agents that connect over MCP.",
	"nav.activity-log":           "Activity log",
	"nav.activity-log.desc":      "What happened on your account.",

	// Command palette.
	"palette.trigger":     "Search",
	"palette.aria":        "Search pages",
	"palette.title":       "Go to page",
	"palette.placeholder": "Search pages…",
	// Split because the query itself renders as a highlighted span between the
	// two halves, and word order around it differs by language.
	"palette.empty.before": "No pages match",
	"palette.empty.after":  ".",

	"palette.hint.navigate": "navigate",
	"palette.hint.open":     "open",
	"palette.hint.close":    "close",
}
