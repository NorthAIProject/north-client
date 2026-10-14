package i18n

// englishCapture is the quick-capture page. Its microphone is the shared
// dictation button, whose copy lives in en_dictate.go.
var englishCapture = map[string]string{
	"capture.heading": "Quick capture",
	"capture.intro":   "Write your day the way you would say it. Nothing is saved until you check it.",
	"capture.ph":      "slept 6h, 2L water, read 20 pages, 78kg, mood 4 energy 3",
	"capture.read":    "Read it",

	// The box embedded on the food log.
	"capture.embed.food.title": "Describe what you ate",
	"capture.embed.food.intro": "Type or dictate a meal. Nothing is saved until you check it.",

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

	"capture.cost":       "Reading it costs a moment. Saving is one tap.",
	"capture.start_over": "Start over",
	"capture.save":       "Save",
	"capture.estimated":  "estimated",

	// The words beside each editable number in the preview.
	"capture.unit.water":  "ml of water",
	"capture.unit.sleep":  "minutes asleep",
	"capture.unit.mood":   "mood",
	"capture.unit.energy": "energy",
	"capture.unit.food":   "g of %[1]s",

	// A meal's "say what's in it" box on the food log.
	"capture.foods.ph":        "Or say what's in it: 200 g chicken breast, 100 g rice and two eggs",
	"capture.foods.find":      "Find ingredients",
	"capture.foods.none":      "No food found in that. Name each food and how much, such as \"150 g salmon\".",
	"capture.foods.add":       "Add to meal",
	"capture.foods.not_added": "Not added:",
	"capture.foods.which":     "Which %[1]s?",
	"capture.foods.ambiguous": "More than one ingredient matches. Choose one, then tick it.",
}
