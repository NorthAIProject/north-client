package i18n

// englishCapture is the quick-capture page, including the copy handed to
// capture-recorder.js on data- attributes so that script holds no English.
var englishCapture = map[string]string{
	"capture.heading": "Quick capture",
	"capture.intro":   "Write your day the way you would say it. Nothing is saved until you check it.",
	"capture.ph":      "slept 6h, 2L water, read 20 pages, 78kg, mood 4 energy 3",
	"capture.read":    "Read it",

	"capture.voice.aria":        "Record a voice note",
	"capture.voice.say":         "Say it",
	"capture.voice.stop":        "Stop",
	"capture.voice.reading":     "Reading it…",
	"capture.voice.empty":       "That recording was empty.",
	"capture.voice.toolong":     "That recording is too long.",
	"capture.voice.unreachable": "That did not reach Khepri. Try again.",
	"capture.voice.unreadable":  "Khepri could not read that recording. Try again.",
	"capture.voice.mic":         "Khepri could not reach the microphone. Check the permission and try again.",
	"capture.voice.unsupported": "This browser cannot record audio.",

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
