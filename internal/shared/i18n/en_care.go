package i18n

// englishCare is the care page: water, sleep, habits and reminders.
var englishCare = map[string]string{
	"care.mono":    "Care",
	"care.heading": "Today",
	"care.intro":   "What the app is looking out for you about, in one place.",

	"care.kpi.water":    "Water",
	"care.kpi.sleep":    "Sleep",
	"care.kpi.habits":   "Habits",
	"care.kpi.due":      "Due",
	"care.kpi.checkin":  "Check-in",
	"care.kpi.none":     "None",
	"care.kpi.done":     "Done",
	"care.kpi.pending":  "Pending",
	"care.kpi.edit":     "Edit",
	"care.kpi.checkcta": "Check in",

	"care.inst.hydration":       "Hydration 7d",
	"care.inst.hydration.empty": "No water logged this week.",
	"care.inst.sleep":           "Sleep 7d",
	"care.inst.sleep.empty":     "Log last night below.",
	"care.inst.habits":          "Habits",
	"care.inst.habits.empty":    "Add a habit below.",

	"care.checkin.title":   "Check-in",
	"care.checkin.done":    "Done for today.",
	"care.checkin.pending": "You haven't checked in yet today.",

	"care.water.title": "Water",
	// Both amounts are already formatted ("1.5L"), hence %[1]s not %[1]f.
	"care.water.desc": "%[1]s of %[2]s today",
	"care.water.undo": "Undo",

	"care.sleep.title": "Sleep",
	"care.sleep.last":  "Last night: %[1]s",
	// Appended to the line above when a quality was recorded.
	"care.sleep.quality.suffix": ", quality %[1]s/5",
	"care.sleep.empty":          "Nothing logged for last night.",
	"care.sleep.hours":          "Hours",
	"care.sleep.minutes":        "Minutes",
	"care.sleep.quality":        "Quality",
	"care.sleep.quality.ph":     "Optional",
	"care.sleep.bedtime":        "Bedtime",
	"care.sleep.woke":           "Woke up",
	"care.sleep.update":         "Update last night",
	"care.sleep.log":            "Log last night",

	"care.habits.title":  "Habits",
	"care.habits.desc":   "Recurring things you meant to keep doing.",
	"care.habits.new":    "New habit",
	"care.habits.new.ph": "Ten minutes of reading",
	"care.habits.area":   "Area",
	"care.habits.days":   "Days",
	"care.habits.add":    "Add habit",
	"care.habits.done":   "Done",
	"care.habits.delete": "Delete",

	"care.rem.title":    "Reminders",
	"care.rem.due":      "Due now.",
	"care.rem.none":     "Nothing due right now.",
	"care.rem.manage":   "Manage reminders",
	"care.rem.label":    "Label",
	"care.rem.label.ph": "Log lunch",
	"care.rem.time":     "Time",
	"care.rem.days":     "Days",
	"care.rem.add":      "Add reminder",
	"care.rem.disable":  "Disable",
	"care.rem.enable":   "Enable",
	"care.rem.delete":   "Delete",
	"care.rem.everyday": "Every day",

	// Three-letter weekday abbreviations, in Go's Weekday order starting at
	// Sunday. Abbreviations rather than full names because they sit in a row of
	// seven checkboxes on a phone.
	"weekday.short.0": "Sun",
	"weekday.short.1": "Mon",
	"weekday.short.2": "Tue",
	"weekday.short.3": "Wed",
	"weekday.short.4": "Thu",
	"weekday.short.5": "Fri",
	"weekday.short.6": "Sat",
}
