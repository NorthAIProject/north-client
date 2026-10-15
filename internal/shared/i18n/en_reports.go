package i18n

// englishReports is the weekly report list and a single report.
//
// The report's title and body are generated text and are not translated here;
// they are written in whatever language the model was asked for.
var englishReports = map[string]string{
	"reports.mono":           "Review",
	"reports.h1":             "Weekly reports",
	"reports.lede":           "A written week, from what you actually recorded.",
	"reports.generate":       "Generate this week",
	"reports.cooldown.long":  "Already generated this hour. Read the current review, or wait a bit.",
	"reports.cooldown":       "Already generated this hour.",
	"reports.current":        "Current",
	"reports.archived":       "Archived",
	"reports.empty.title":    "No review yet",
	"reports.empty.body":     "A written week, from what you actually recorded — check-ins, goals, and training.",
	"reports.status.pending": "pending",
	"reports.status.ready":   "ready",
	"reports.status.failed":  "failed",
	"reports.archive":        "Archive",
	"reports.regenerate":     "Regenerate",
	"reports.isarchived":     "This review is archived. Generate a new one from the list instead.",
	"reports.writing":        "Writing this week… refresh in a moment.",
	"reports.failed":         "That pass failed.",
	"reports.useful.yes":     "Marked useful. Thank you.",
	"reports.useful.no":      "Marked not useful.",
	"reports.useful.ask":     "Was this useful?",
	"reports.undo":           "Undo",
	"reports.label.pending":  "Generating",
	"reports.label.failed":   "Failed",
	"reports.label.ready":    "Ready",
	"reports.label.readyon":  "Ready · %[1]s",
}
