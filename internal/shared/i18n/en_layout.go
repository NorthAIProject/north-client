package i18n

// englishLayout is the application shell and the small shared components
// under web/shared: the sidebar footer, the auth card, the workout recap, the
// muscle readiness card, the moment card and the exercise artwork credit.
var englishLayout = map[string]string{
	"layout.signout":      "Sign out",
	"layout.auth.tagline": "Continue your thread",
	"layout.palette.list": "Pages",
	"layout.moment.label": "Moment",

	"layout.recap.time":          "Time",
	"layout.recap.sets":          "Sets",
	"layout.recap.volume":        "Volume",
	"layout.recap.burned":        "Burned",
	"layout.recap.est":           "est. %[1]s",
	"layout.recap.last":          "Last workout",
	"layout.recap.series.now":    "This session",
	"layout.recap.series.before": "Last time",

	"layout.week.done":     "%[1]s: completed",
	"layout.week.today":    "%[1]s: today, not done yet",
	"layout.week.missed":   "%[1]s: missed",
	"layout.week.upcoming": "%[1]s: coming up",

	"layout.readiness.title":      "Muscles",
	"layout.readiness.desc":       "From your logged sets.",
	"layout.readiness.last":       "Last session %[1]s",
	"layout.readiness.fatigued":   "Fatigued",
	"layout.readiness.recovering": "Recovering",
	"layout.readiness.trained":    "Trained this week",
	"layout.readiness.stale":      "Going stale:",
	"layout.readiness.stale.days": "%[1]s · %[2]d days",

	"layout.art.aria":            "Illustration of %[1]s",
	"layout.art.credit.by":       "Exercise illustrations by",
	"layout.art.credit.and":      "and",
	"layout.art.credit.licensed": "licensed",
}
