package i18n

// englishInbox is the capture inbox: save now, file later. The items and the
// coach's reason for each suggestion are not copy.
var englishInbox = map[string]string{
	"inbox.title": "Inbox",
	"inbox.label": "Capture",
	"inbox.intro": "Save now, decide later. The coach suggests where each thing belongs; nothing is filed until you say so.",
	"inbox.ph":    "A thought, a link, something that happened…",
	"inbox.save":  "Save to Inbox",
	"inbox.empty": "Nothing waiting.",

	"inbox.waiting": "%[1]d waiting",
	"inbox.pending": "The coach is still looking at this one.",

	// The suggestion line: where it belongs, then the coach's reason.
	"inbox.suggested":                "Suggested: %[1]s. %[2]s",
	"inbox.suggest.goal":             "A note on %[1]s",
	"inbox.suggest.knowledge_titled": "Knowledge: %[1]s",
	"inbox.file_suggested":           "File as suggested",

	"inbox.dest.journal":   "Journal",
	"inbox.dest.knowledge": "Knowledge",
	"inbox.dest.goal":      "Goal note",
	"inbox.goal_ph":        "A goal",
	"inbox.dismiss":        "Dismiss",
}
