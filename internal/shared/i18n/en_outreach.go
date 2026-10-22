package i18n

// englishOutreach is the coach speaking first: its message title, the answers under
// it, what each answer did, and the fallback when the coach could not write.
var englishOutreach = map[string]string{
	"outreach.title":                    "From your coach",
	"outreach.action.undo":              "Undo",
	"outreach.action.lighter":           "Take it lighter",
	"outreach.action.keep":              "Keep the plan",
	"outreach.action.today":             "Do it today",
	"outreach.action.tomorrow":          "Tomorrow",
	"outreach.action.let_go":            "Let it go",
	"outreach.done.undo":                "Undone: today is back to the plan.",
	"outreach.done.lighter":             "Done: today is lighter, about 60% of the sets.",
	"outreach.done.keep":                "Kept: today stays as planned.",
	"outreach.done.today":               "Moved: it's on today.",
	"outreach.done.tomorrow":            "Moved: it's on tomorrow.",
	"outreach.done.let_go":              "Let go: the plan picks it up next time.",
	"outreach.fallback.lighter.applied": "Your recovery is under your usual this morning, so I cut %s to about 60% of the sets. Undo it if you'd rather train as planned.",
	"outreach.fallback.lighter.offer":   "Your recovery is under your usual this morning. Want to take %s lighter today, about 60% of the sets?",
	"outreach.fallback.missed":          "%s didn't happen yesterday. Move it, or let it go?",
	"outreach.error.action":             "That answer isn't available for this message.",
	"outreach.error.answered":           "That was already answered.",
	"outreach.error":                    "That couldn't be done just now. Try again from the app.",
	"outreach.action.done":              "Done",
	"outreach.done.done":                "Marked done for today.",
	"outreach.fallback.firstweek":       "Day %[1]d of 7. Today's one thing: %[2]s",
}
