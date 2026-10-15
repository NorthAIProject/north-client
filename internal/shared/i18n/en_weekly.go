package i18n

// englishWeekly is the weekly review: last week's report, then the plan for the
// next one.
var englishWeekly = map[string]string{
	"weekly.title":              "Weekly review",
	"weekly.h1":                 "Plan the week of %[1]s",
	"weekly.lede":               "How the week of %[1]s went, then up to three priorities, your goals in order, and how hard to train.",
	"weekly.saved":              "Saved. The coach, your goals and your training plan use this week's focus now.",
	"weekly.happened":           "The week that happened",
	"weekly.chose":              "You chose: %[1]s",
	"weekly.writing":            "Your weekly report is being written. Reload in a minute, or plan the week now.",
	"weekly.priorities":         "Priorities",
	"weekly.priorities.desc":    "Up to three things this week is for.",
	"weekly.priority":           "Priority %[1]d",
	"weekly.goals":              "Goals in order",
	"weekly.goals.desc":         "1 is first. The coach and your goals list follow this order.",
	"weekly.goals.order":        "Order of %[1]s",
	"weekly.training":           "Training this week",
	"weekly.volume.hold":        "Hold",
	"weekly.volume.hold.hint":   "Train the plan as written.",
	"weekly.volume.build":       "Build",
	"weekly.volume.build.hint":  "One extra set on each day's first two exercises.",
	"weekly.volume.deload":      "Deload",
	"weekly.volume.deload.hint": "About 60% of the sets, to recover.",
	"weekly.save":               "Save the week",

	// Errors from internal/weekly. The field errors' English must match what
	// its service writes, which its fieldmsg table test checks.
	"weekly.err.priority.long": "Keep each priority under 120 characters.",
	"weekly.err.priority.many": "Pick at most three priorities.",
	"weekly.err.volume":        "Choose hold, build or deload.",
	"weekly.err.read":          "That request could not be read.",
	"weekly.err.generic":       "Something went wrong.",
	"weekly.api.review":        "The weekly review could not be loaded.",
	"weekly.api.body":          "The request body could not be read.",
	"weekly.api.save":          "The week's focus could not be saved.",
}
