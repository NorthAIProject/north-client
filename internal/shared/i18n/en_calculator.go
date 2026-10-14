package i18n

// englishCalculator is the calorie and macro calculator. Options are keyed by
// the macroplan enum value, each with a short label and a description; the
// descriptions mirror macroplan's own, which the coach still reads in English.
var englishCalculator = map[string]string{
	"calculator.page":    "Calculator",
	"calculator.heading": "Calorie & macro calculator",
	"calculator.intro":   "Mifflin-St Jeor, scaled by your own activity and goal.",

	"calculator.bio.title":   "Your measurements",
	"calculator.bio.current": "%[1]s %[2]s, %[3]s %[4]s, %[5]d years old.",
	"calculator.bio.empty":   "Needed before a plan can be generated.",
	"calculator.bio.weight":  "Weight (%[1]s)",
	"calculator.bio.height":  "Height (%[1]s)",
	"calculator.bio.dob":     "Date of birth",
	"calculator.bio.sex":     "Sex",
	"calculator.bio.choose":  "Choose one",
	"calculator.bio.male":    "Male",
	"calculator.bio.female":  "Female",
	"calculator.bio.save":    "Save measurements",

	"calculator.plan.title":    "Generate a plan",
	"calculator.plan.summary":  "%[1]s target: %[2]s kcal/day (BMR %[3]s, TDEE %[4]s) — %[5]s split → %[6]sg protein / %[7]sg fat / %[8]sg carbs",
	"calculator.plan.empty":    "Record your measurements above first.",
	"calculator.plan.activity": "Activity level",
	"calculator.plan.goal":     "Goal",
	"calculator.plan.split":    "Macro split",
	"calculator.plan.submit":   "Generate plan",

	"calculator.activity.sedentary":        "Sedentary",
	"calculator.activity.sedentary.desc":   "Desk job, little activity beyond daily life.",
	"calculator.activity.light":            "Light",
	"calculator.activity.light.desc":       "Training one or two days a week.",
	"calculator.activity.moderate":         "Moderate",
	"calculator.activity.moderate.desc":    "Training three to five days a week.",
	"calculator.activity.heavy":            "Heavy",
	"calculator.activity.heavy.desc":       "Training six or seven days a week.",
	"calculator.activity.extra_heavy":      "Extra heavy",
	"calculator.activity.extra_heavy.desc": "Training twice a day, or physical work on top of training.",

	"calculator.goal.cutting":          "Cutting",
	"calculator.goal.cutting.desc":     "Lose weight, at roughly 0.45 kg (1 lb) a week.",
	"calculator.goal.maintenance":      "Maintenance",
	"calculator.goal.maintenance.desc": "Hold your current weight.",
	"calculator.goal.bulking":          "Bulking",
	"calculator.goal.bulking.desc":     "Gain weight, at roughly 0.35 kg (0.75 lb) a week.",

	"calculator.split.high_carb":           "High carb",
	"calculator.split.high_carb.short":     "high-carb",
	"calculator.split.high_carb.desc":      "30% protein, 20% fat, 50% carbs.",
	"calculator.split.moderate_carb":       "Moderate carb",
	"calculator.split.moderate_carb.short": "moderate-carb",
	"calculator.split.moderate_carb.desc":  "30% protein, 35% fat, 35% carbs.",
	"calculator.split.low_carb":            "Low carb",
	"calculator.split.low_carb.short":      "low-carb",
	"calculator.split.low_carb.desc":       "40% protein, 40% fat, 20% carbs.",

	"calculator.compare.title": "What each goal would cost",
	"calculator.compare.desc":  "Against a TDEE of %[1]s kcal a day.",
	"calculator.compare.yours": "Yours",
}
