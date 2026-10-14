package i18n

// englishExercises is the exercise catalog: the browse list, the detail page,
// and the vocabulary the catalog stores (muscles, types, levels, equipment).
// Exercise names themselves stay English: the catalog only has them in English.
var englishExercises = map[string]string{
	"exercises.browse.lede":         "What each movement trains, shown on the model. The coach picks from this same catalog when it writes you a plan.",
	"exercises.browse.count":        "%[1]d exercises",
	"exercises.browse.range":        "Showing %[1]d-%[2]d of %[3]d exercises",
	"exercises.browse.none":         "Nothing matched. Try a broader filter.",
	"exercises.browse.clearfilters": "Clear filters",

	"exercises.filter.search":    "Search by name",
	"exercises.filter.muscle":    "Any muscle",
	"exercises.filter.category":  "Any type",
	"exercises.filter.equipment": "Any equipment",
	"exercises.filter.submit":    "Filter",
	"exercises.filter.clear":     "Clear",

	"exercises.page.prev": "Previous",
	"exercises.page.next": "Next",

	"exercises.detail.primary":   "Primary",
	"exercises.detail.secondary": "Secondary",
	"exercises.detail.video":     "Watch the movement",
	"exercises.detail.howto":     "How to do it",

	"exercises.next.label":  "What next",
	"exercises.next.noplan": "Build a training plan and the coach will pick movements like this one for you.",
	"exercises.next.build":  "Build a training plan",
	"exercises.next.inplan": "Already in your plan on %[1]s.",
	"exercises.next.open":   "Open your plan",
	"exercises.next.add":    "Not in your plan yet. Add it to a day:",

	"exercises.similar.title": "Also trains %[1]s",
	"exercises.similar.all":   "See all",

	// The caption reads "What <exercise> trains."; the name is a link, so the
	// sentence is split around it.
	"exercises.partial.before": "What",
	"exercises.partial.after":  " trains.",

	"exercises.muscle.quads":      "Quads",
	"exercises.muscle.glutes":     "Glutes",
	"exercises.muscle.hamstrings": "Hamstrings",
	"exercises.muscle.calves":     "Calves",
	"exercises.muscle.adductors":  "Adductors",
	"exercises.muscle.traps":      "Traps",
	"exercises.muscle.delts":      "Delts",
	"exercises.muscle.biceps":     "Biceps",
	"exercises.muscle.triceps":    "Triceps",
	"exercises.muscle.forearms":   "Forearms",
	"exercises.muscle.lats":       "Lats",
	"exercises.muscle.rhomboids":  "Rhomboids",
	"exercises.muscle.erectors":   "Erectors",
	"exercises.muscle.serratus":   "Serratus",
	"exercises.muscle.abs":        "Abs",
	"exercises.muscle.chest":      "Chest",
	"exercises.muscle.neck":       "Neck",

	"exercises.category.strength":              "Strength",
	"exercises.category.cardio":                "Cardio",
	"exercises.category.stretching":            "Stretching",
	"exercises.category.plyometrics":           "Plyometrics",
	"exercises.category.powerlifting":          "Powerlifting",
	"exercises.category.olympic_weightlifting": "Olympic weightlifting",
	"exercises.category.strongman":             "Strongman",

	"exercises.difficulty.beginner":     "Beginner",
	"exercises.difficulty.intermediate": "Intermediate",
	"exercises.difficulty.expert":       "Expert",

	"exercises.equipment.none":            "None",
	"exercises.equipment.barbell":         "Barbell",
	"exercises.equipment.dumbbell":        "Dumbbell",
	"exercises.equipment.kettlebell":      "Kettlebell",
	"exercises.equipment.machine":         "Machine",
	"exercises.equipment.bench":           "Bench",
	"exercises.equipment.pull_up_bar":     "Pull-up bar",
	"exercises.equipment.resistance_band": "Resistance band",
	"exercises.equipment.bike":            "Bike",
	"exercises.equipment.other":           "Other",
}
