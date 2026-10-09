package exercises

// otherAppNames maps exercise names as other training apps export them to
// this catalog's slugs, for the ones the word match in Matcher cannot settle
// alone: the catalog says "Barbell Full Squat" where Hevy says
// "Squat (Barbell)", and several catalog rows share a short name's words.
//
// Written from Hevy's naming ("Movement (Equipment)"), which Strong mostly
// shares. Keys are compared by their words, so case, brackets and plurals do
// not matter. Every slug must exist in the catalog; TestAliasesPointAtCatalog
// checks it against the seeded table.
var otherAppNames = map[string]string{
	"Bench Press (Barbell)":              "barbell-bench-press-medium-grip",
	"Bench Press (Dumbbell)":             "dumbbell-bench-press",
	"Bench Press (Smith Machine)":        "smith-machine-bench-press",
	"Close Grip Bench Press":             "close-grip-bench-press",
	"Decline Bench Press (Barbell)":      "decline-bench-press",
	"Incline Bench Press (Barbell)":      "incline-bench-press",
	"Incline Bench Press (Dumbbell)":     "incline-dumbbell-bench-press",
	"Chest Press (Machine)":              "machine-chest-press",
	"Chest Fly (Dumbbell)":               "dumbbell-fly",
	"Cable Fly Crossovers":               "cable-fly",
	"Butterfly (Pec Deck)":               "pec-deck",
	"Push Up":                            "push-up",
	"Chest Dip":                          "chest-dip",
	"Triceps Dip":                        "triceps-dip",
	"Squat (Barbell)":                    "barbell-full-squat",
	"Squat (Smith Machine)":              "smith-machine-squat",
	"Squat (Bodyweight)":                 "bodyweight-squat",
	"Front Squat":                        "front-squat",
	"Goblet Squat":                       "goblet-squat",
	"Hack Squat (Machine)":               "hack-squat",
	"Bulgarian Split Squat":              "bulgarian-split-squat",
	"Leg Press (Machine)":                "leg-press",
	"Leg Extension (Machine)":            "leg-extension",
	"Lying Leg Curl (Machine)":           "lying-leg-curl",
	"Seated Leg Curl (Machine)":          "seated-leg-curl",
	"Lunge (Dumbbell)":                   "forward-lunge",
	"Walking Lunge":                      "walking-lunge",
	"Deadlift (Barbell)":                 "barbell-deadlift",
	"Deadlift (Trap Bar)":                "trap-bar-deadlift",
	"Sumo Deadlift":                      "sumo-deadlift",
	"Romanian Deadlift (Barbell)":        "romanian-deadlift",
	"Romanian Deadlift (Dumbbell)":       "dumbbell-romanian-deadlift",
	"Hip Thrust (Barbell)":               "barbell-hip-thrust",
	"Good Morning (Barbell)":             "good-morning",
	"Standing Calf Raise (Machine)":      "standing-calf-raise",
	"Seated Calf Raise":                  "seated-calf-raise",
	"Overhead Press (Barbell)":           "overhead-press",
	"Shoulder Press (Dumbbell)":          "arnold-press",
	"Shoulder Press (Machine Plates)":    "machine-shoulder-press",
	"Arnold Press (Dumbbell)":            "arnold-press",
	"Lateral Raise (Dumbbell)":           "lateral-raise",
	"Lateral Raise (Cable)":              "cable-lateral-raise",
	"Lateral Raise (Machine)":            "machine-lateral-raise",
	"Rear Delt Reverse Fly (Dumbbell)":   "rear-delt-fly",
	"Reverse Fly (Machine)":              "reverse-pec-deck",
	"Face Pull":                          "face-pull",
	"Upright Row (Barbell)":              "upright-row",
	"Shrug (Barbell)":                    "barbell-shrug",
	"Shrug (Dumbbell)":                   "standing-dumbbell-shrug",
	"Bent Over Row (Barbell)":            "barbell-row",
	"Dumbbell Row":                       "one-arm-dumbbell-row",
	"Pendlay Row (Barbell)":              "pendlay-row",
	"T Bar Row":                          "t-bar-row",
	"Seated Cable Row - V Grip (Cable)":  "seated-cable-rows",
	"Seated Row (Machine)":               "machine-row",
	"Lat Pulldown (Cable)":               "lat-pulldown",
	"Lat Pulldown - Close Grip (Cable)":  "close-grip-front-lat-pulldown",
	"Straight Arm Lat Pulldown (Cable)":  "straight-arm-pulldown",
	"Pull Up":                            "pull-up",
	"Pull Up (Weighted)":                 "weighted-pull-up",
	"Pull Up (Assisted)":                 "assisted-pull-up",
	"Chin Up":                            "chin-up",
	"Inverted Row":                       "inverted-row",
	"Bicep Curl (Barbell)":               "barbell-curl",
	"Bicep Curl (Dumbbell)":              "bicep-curl",
	"Bicep Curl (Cable)":                 "cable-curl",
	"EZ Bar Biceps Curl":                 "ez-bar-curl",
	"Hammer Curl (Dumbbell)":             "hammer-curl",
	"Preacher Curl (Barbell)":            "preacher-curl",
	"Concentration Curl":                 "concentration-curl",
	"Triceps Pushdown":                   "tricep-pushdown",
	"Triceps Rope Pushdown":              "rope-tricep-pushdown",
	"Skullcrusher (Barbell)":             "ez-bar-skullcrusher",
	"Skullcrusher (Dumbbell)":            "dumbbell-skull-crusher",
	"Overhead Triceps Extension (Cable)": "overhead-tricep-extension",
	"Triceps Extension (Dumbbell)":       "dumbbell-overhead-tricep-extension",
	"Plank":                              "elbow-plank",
	"Crunch":                             "crunch",
	"Cable Crunch":                       "cable-crunch",
	"Hanging Leg Raise":                  "hanging-leg-raise",
	"Lying Leg Raise":                    "lying-leg-raise",
	"Russian Twist (Weighted)":           "weighted-russian-twist",
	"Russian Twist (Bodyweight)":         "russian-twist",
	"Ab Wheel":                           "ab-wheel",
	"Hip Abduction (Machine)":            "hip-abduction-machine",
	"Hip Adduction (Machine)":            "hip-adduction-machine",
}

// aliases is otherAppNames keyed by words, as Matcher compares them.
var aliases = func() map[string]string {
	out := make(map[string]string, len(otherAppNames))
	for name, slug := range otherAppNames {
		out[wordsKey(nameWords(name))] = slug
	}
	return out
}()
