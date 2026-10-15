package i18n

// englishStats is the wording of what internal/stats finds, for a person to
// read: pattern findings, personal-best labels and activity-type names.
//
// The stats package writes these same sentences in English for the coach,
// which reads them through its tools and must keep reading English. These are
// the person's copy, applied by stat.Finding.In, stat.KindStats.In and
// stats.KindLabel at the point the text is shown — and the English here has to
// match what the package writes, which stat's tests hold it to.
//
// %[1]s is the size of a difference or a group's mean, already in the reader's
// notation; %[2]d and %[4]d are day counts; %[3]s the other group's mean;
// %[2]s in a title and %[5]s in a detail is the line a split was drawn at.
var englishStats = map[string]string{
	"stats.pattern.sleep_late_caffeine.title.up":   "You sleep %[1]s more after late caffeine",
	"stats.pattern.sleep_late_caffeine.title.down": "You sleep %[1]s less after late caffeine",
	"stats.pattern.sleep_late_caffeine.detail":     "%[1]s on %[2]d nights after caffeine past your cutoff, %[3]s on %[4]d other nights.",
	"stats.pattern.sleep_late_caffeine.a":          "After caffeine past your cutoff",
	"stats.pattern.sleep_late_caffeine.b":          "Other nights",

	"stats.pattern.sleep_late_eating.title.up":   "You sleep %[1]s more after eating late",
	"stats.pattern.sleep_late_eating.title.down": "You sleep %[1]s less after eating late",
	"stats.pattern.sleep_late_eating.detail":     "%[1]s on %[2]d nights after food past your kitchen-closes time, %[3]s on %[4]d nights after an earlier last meal.",
	"stats.pattern.sleep_late_eating.a":          "After eating late",
	"stats.pattern.sleep_late_eating.b":          "After an earlier last meal",

	"stats.pattern.energy_sleep.title.up":   "Your energy is %[1]s points higher after 7+ hours of sleep",
	"stats.pattern.energy_sleep.title.down": "Your energy is %[1]s points lower after 7+ hours of sleep",
	"stats.pattern.energy_sleep.detail":     "Check-in energy averages %[1]s on %[2]d days after 7+ hours, %[3]s on %[4]d days after less.",
	"stats.pattern.energy_sleep.a":          "After 7 hours or more",
	"stats.pattern.energy_sleep.b":          "After less than 7 hours",

	"stats.pattern.mood_training.title.up":   "Your mood is %[1]s points higher on training days",
	"stats.pattern.mood_training.title.down": "Your mood is %[1]s points lower on training days",
	"stats.pattern.mood_training.detail":     "Check-in mood averages %[1]s on %[2]d training days, %[3]s on %[4]d rest days.",
	"stats.pattern.mood_training.a":          "Training days",
	"stats.pattern.mood_training.b":          "Rest days",

	"stats.pattern.mood_sleep.title.up":   "Your mood is %[1]s points higher after 7+ hours of sleep",
	"stats.pattern.mood_sleep.title.down": "Your mood is %[1]s points lower after 7+ hours of sleep",
	"stats.pattern.mood_sleep.detail":     "Check-in mood averages %[1]s on %[2]d days after 7+ hours, %[3]s on %[4]d days after less.",
	"stats.pattern.mood_sleep.a":          "After 7 hours or more",
	"stats.pattern.mood_sleep.b":          "After less than 7 hours",

	"stats.pattern.mood_outdoor.title.up":   "Your mood is %[1]s points higher on days you train outdoors",
	"stats.pattern.mood_outdoor.title.down": "Your mood is %[1]s points lower on days you train outdoors",
	"stats.pattern.mood_outdoor.detail":     "Check-in mood averages %[1]s on %[2]d days with an outdoor workout, %[3]s on %[4]d other days.",
	"stats.pattern.mood_outdoor.a":          "Days you trained outdoors",
	"stats.pattern.mood_outdoor.b":          "Other days",

	"stats.pattern.hrv_training.title.up":   "Your HRV is %[1]s ms higher the day after training",
	"stats.pattern.hrv_training.title.down": "Your HRV is %[1]s ms lower the day after training",
	"stats.pattern.hrv_training.detail":     "HRV averages %[1]s ms on %[2]d days after training, %[3]s ms on %[4]d days after rest.",
	"stats.pattern.hrv_training.a":          "The day after training",
	"stats.pattern.hrv_training.b":          "The day after rest",

	"stats.pattern.rhr_training.title.up":   "Your resting heart rate is %[1]s bpm higher the day after training",
	"stats.pattern.rhr_training.title.down": "Your resting heart rate is %[1]s bpm lower the day after training",
	"stats.pattern.rhr_training.detail":     "Resting heart rate averages %[1]s bpm on %[2]d days after training, %[3]s bpm on %[4]d days after rest.",
	"stats.pattern.rhr_training.a":          "The day after training",
	"stats.pattern.rhr_training.b":          "The day after rest",

	"stats.pattern.energy_training.title.up":   "Your energy is %[1]s points higher on training days",
	"stats.pattern.energy_training.title.down": "Your energy is %[1]s points lower on training days",
	"stats.pattern.energy_training.detail":     "Check-in energy averages %[1]s on %[2]d training days, %[3]s on %[4]d rest days.",
	"stats.pattern.energy_training.a":          "Training days",
	"stats.pattern.energy_training.b":          "Rest days",

	"stats.pattern.sleep_screen.title.up":   "You sleep %[1]s more after days with more than %[2]s of screen time",
	"stats.pattern.sleep_screen.title.down": "You sleep %[1]s less after days with more than %[2]s of screen time",
	"stats.pattern.sleep_screen.detail":     "%[1]s on %[2]d nights after heavier screen days, %[3]s on %[4]d nights after lighter ones.",
	"stats.pattern.sleep_screen.a":          "After heavy screen days",
	"stats.pattern.sleep_screen.b":          "After lighter ones",

	"stats.pattern.mood_measure.title.up":   "Your mood is %[1]s points higher on days with %[2]s",
	"stats.pattern.mood_measure.title.down": "Your mood is %[1]s points lower on days with %[2]s",
	"stats.pattern.mood_measure.detail":     "Check-in mood averages %[1]s on %[2]d days with %[5]s, %[3]s on %[4]d other days.",
	"stats.pattern.mood_measure.a":          "Days with %[1]s",
	"stats.pattern.mood_measure.b":          "Other days",

	"stats.pattern.mood_steps.what": "%[1]s steps or more",

	"stats.pattern.mood_daylight.what": "%[1]s or more in daylight",

	"stats.pattern.mood_stand.what": "%[1]s stand hours or more",

	"stats.pattern.mood_mindful.title.up":   "Your mood is %[1]s points higher on days you take a mindful moment",
	"stats.pattern.mood_mindful.title.down": "Your mood is %[1]s points lower on days you take a mindful moment",
	"stats.pattern.mood_mindful.detail":     "Check-in mood averages %[1]s on %[2]d days with a mindful session, %[3]s on %[4]d days without.",
	"stats.pattern.mood_mindful.a":          "Days with a mindful session",
	"stats.pattern.mood_mindful.b":          "Days without",

	"stats.best.fastest_5k": "Fastest 5 km",

	"stats.best.fastest_10k": "Fastest 10 km",

	"stats.best.longest_run": "Longest run",

	"stats.best.longest_ride": "Longest ride",

	"stats.best.fastest_20k": "Fastest ride over 20 km",

	"stats.best.most_climb": "Most climbing",

	"stats.best.longest_walk": "Longest walk",

	"stats.best.longest_swim": "Longest swim",

	"stats.best.longest_session": "Longest session",

	"stats.kind.running": "Running",

	"stats.kind.cycling": "Cycling",

	"stats.kind.walking": "Walking",

	"stats.kind.swimming": "Swimming",

	"stats.kind.indoor_running": "Indoor running",

	"stats.kind.indoor_cycling": "Indoor cycling",
}
