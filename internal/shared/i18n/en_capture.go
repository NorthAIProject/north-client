package i18n

// englishCapture is the quick-capture page. Its microphone is the shared
// dictation button, whose copy lives in en_dictate.go.
var englishCapture = map[string]string{
	"capture.heading": "Quick capture",
	"capture.intro":   "Write your day the way you would say it. Nothing is saved until you check it.",
	"capture.ph":      "slept 6h, 2L water, read 20 pages, 78kg, mood 4 energy 3",
	"capture.read":    "Read it",

	"capture.left.title": "Not logged",
	"capture.left.body":  "I could not read these as entries, so nothing was saved for them.",
	"capture.left.ask":   "Ask the coach",
	"capture.left.note":  "Save as a journal note",

	// The receipt. Three shapes rather than one format string: the singular is
	// not "1 things", and the partial case reads differently everywhere.
	"capture.logged.one":     "Logged 1 thing.",
	"capture.logged.many":    "Logged %[1]d things.",
	"capture.logged.partial": "Logged %[1]d of %[2]d.",

	"capture.kind.water":   "Water",
	"capture.kind.sleep":   "Sleep",
	"capture.kind.habit":   "Habit",
	"capture.kind.weight":  "Weight",
	"capture.kind.checkin": "Check-in",
	"capture.kind.food":    "Food",
}
