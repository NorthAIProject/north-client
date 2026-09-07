package i18n

// englishGoals is the goals pages, plus the goal domain's category and status
// labels.
var englishGoals = map[string]string{
	"goals.mono":  "Direction",
	"goals.intro": "What you are working toward. Khepri reads these before every reply.",
	"goals.add":   "Add a goal",

	"goals.empty.title": "Nothing to aim at yet",
	"goals.empty.body":  "The coach works better once it knows what you are aiming at. One goal is enough to start.",

	"goals.kpi.active":   "Active",
	"goals.kpi.progress": "Avg progress",
	"goals.kpi.overdue":  "Overdue milestones",
	"goals.kpi.none":     "None",

	"goals.bycategory": "Active by category",
	"goals.achieved":   "Achieved",
	"goals.overdue":    "Overdue",

	"goals.edit":   "Edit",
	"goals.save":   "Save",
	"goals.cancel": "Cancel",
	"goals.remove": "Remove",
	"goals.delete": "Delete this goal",

	"goals.milestones":      "Milestones",
	"goals.milestones.desc": "The steps that make this goal feel alive. Optional, but the coach can see them.",
	"goals.ms.reopen":       "Reopen",
	"goals.ms.complete":     "Complete",
	"goals.ms.title":        "Milestone",
	"goals.ms.add":          "Add a milestone",
	"goals.ms.date":         "Target date",
	"goals.ms.title.ph":     "Run 5k without stopping",
	"goals.progress.ph":     "Ran 5k three times this week without stopping.",

	"goals.progress.q":    "How is it going?",
	"goals.progress.desc": "Khepri reads your most recent note, so this is how the coach stays current.",

	"goals.action.achieved":   "Mark achieved",
	"goals.action.pause":      "Pause",
	"goals.action.letgo":      "Let this one go",
	"goals.action.reactivate": "Make active again",

	"goals.f.title":         "What is the goal?",
	"goals.f.title.ph":      "Run 10k without stopping",
	"goals.f.motivation":    "Why does it matter?",
	"goals.f.motivation.ph": "I want to stop feeling out of breath on the stairs.",
	"goals.f.success":       "How will you know it happened?",
	"goals.f.success.ph":    "I run 10k, at any pace, without walking.",
	"goals.f.category":      "Category",
	"goals.f.category.ph":   "Choose one",
	"goals.f.date":          "Target date",
	"goals.f.date.desc":     "Optional. Plenty of goals do not have one.",

	// goal.Category and goal.Status. Keyed off the stored value, which stays an
	// identifier.

	"goal.status.achieved":  "Achieved",
	"goal.status.paused":    "Paused",
	"goal.status.abandoned": "Let go",
	"goal.status.active":    "Active",
}
