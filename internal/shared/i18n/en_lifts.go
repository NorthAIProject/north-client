package i18n

// englishLifts is the set logger and the Hevy import page.
var englishLifts = map[string]string{
	"lifts.today.title":  "Log a set",
	"lifts.today.import": "Import from Hevy",
	"lifts.today.desc":   "Each set feeds your records, the muscle map and what the coach knows.",

	"lifts.form.exercise":    "Exercise",
	"lifts.form.exercise.ph": "Bench press",
	"lifts.form.kg":          "Kg",
	"lifts.form.reps":        "Reps",
	"lifts.form.rir":         "In reserve",
	"lifts.form.rir.help":    "In reserve: reps you had left. 0 is to failure; leave it empty if unsure.",
	"lifts.form.kind":        "Kind of set",
	"lifts.form.submit":      "Log set",
	"lifts.kind.work":        "Work",
	"lifts.kind.warmup":      "Warm-up",
	"lifts.kind.drop":        "Drop set",
	"lifts.list.aria":        "Today's sets",
	"lifts.list.drop":        "Drop",
	"lifts.list.remove":      "Remove %[1]s %[2]s",

	"lifts.import.page":  "Import from Hevy",
	"lifts.import.label": "Import",
	"lifts.import.title": "Bring your Hevy history",
	"lifts.import.lede":  "Every workout becomes a session here, with its sets, warm-ups and effort, so your records, the muscle map and the coach start from what you have already done.",
	"lifts.import.step1": "In Hevy: Profile → Settings → Export & Import Data → Export Workouts.",
	"lifts.import.step2": "Save the CSV, then drop it here. Up to 10 MB.",
	"lifts.import.step3": "Importing the same file again adds nothing twice.",
	"lifts.import.back":  "Back to training",
	"lifts.import.drop":  "Drop your Hevy CSV, or browse",
	"lifts.import.busy":  "Importing your workouts…",

	"lifts.result.already":    "Already imported",
	"lifts.result.imported":   "Imported %[1]d workouts, %[2]d sets",
	"lifts.result.duplicates": "%[1]d workouts were already here and were left as they were.",
	"lifts.result.skipped":    "%[1]d rows were not lifting sets (cardio, timed holds, or impossible numbers) and were skipped.",
	"lifts.result.unmatched":  "These exercises are not in the catalog, so they count toward records and volume but not the muscle map:",
}
