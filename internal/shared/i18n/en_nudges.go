package i18n

// englishNudges is the coach-notes bell and card.
var englishNudges = map[string]string{
	"nudges.title":       "Coach notes",
	"nudges.bell.unread": "Coach notes, unread",
	"nudges.empty":       "No nudges right now.",
	"nudges.dismiss":     "Dismiss",
	// Two forms, because "new" agrees in number in Spanish and Portuguese.
	"nudges.unread.one":  "1 new",
	"nudges.unread.many": "%[1]d new",

	// What the sweep raises. Stored with the nudge in the recipient's
	// language, and sent as is to the bell, Telegram and the lock screen.
	"nudges.evening.title":         "How did today go?",
	"nudges.evening.body":          "Two taps: how you feel, and one thing that went well.",
	"nudges.evening.body.journal":  "You checked in. One line in the journal about today?",
	"nudges.decision.title":        "Did it hold?",
	"nudges.decision.body.month":   "A month ago you decided: %[1]s. How did it turn out?",
	"nudges.decision.body.quarter": "Three months ago you decided: %[1]s. How did it turn out?",
	"nudges.week.title":            "Plan next week",
	"nudges.week.body":             "Two minutes: how this week went, and up to three things for the next.",
	"nudges.streak.title":          "Your %[1]d-day streak ends at midnight",
	"nudges.streak.body":           "One check-in keeps it. Thirty seconds.",
	"nudges.missed.title":          "Check in with yourself",
	"nudges.missed.body.days":      "It has been %[1]d days since your last check-in.",
	"nudges.missed.body.never":     "You have not checked in since joining.",
	// The goal's title, then the weekday it is due.
	"nudges.goal.title":         "“%[1]s” is due %[2]s",
	"nudges.goal.body.today":    "Due today.",
	"nudges.goal.body.tomorrow": "Due tomorrow.",
	"nudges.goal.body.days":     "Due in %[1]d days.",
	"nudges.crew.one":           "%[1]s checked in today",
	"nudges.crew.two":           "%[1]s and %[2]s checked in today",
	"nudges.crew.many":          "%[1]s, %[2]s and %[3]d more checked in today",
	"nudges.crew.body":          "Your turn: two taps.",
	"nudges.meal.body":          "Log what you ate when you get a moment.",
	// The dose's scheduled time, "08:00".
	"nudges.medication.body":          "Your %[1]s dose. Mark it taken or skipped when you can.",
	"nudges.firstweek.check.title":    "How did yesterday go?",
	"nudges.firstweek.check.body":     "Thirty seconds. What happened, or send a photo of where you are now.",
	"nudges.firstweek.evidence.title": "Send one photo",
	"nudges.firstweek.evidence.body":  "A lift, a meal, or how you look today. I will coach from what I see.",
	"nudges.firstweek.review.title":   "Your first week",
	"nudges.firstweek.review.body":    "Open the coach and tell me what stuck. I will keep what matters.",
	"nudges.workout.title":            "Start today's session",
	"nudges.workout.default":          "Today's session",
	"nudges.photo.ask.title":          "Send a photo",
	"nudges.photo.ask.body":           "Your photo check-in is due. A lift, a meal, or how you look today.",
	"nudges.photo.reminder.title":     "Still waiting on that photo",
	"nudges.photo.reminder.body":      "A reminder: send one photo so I can coach from what I see.",

	"nudges.err.load":     "Notifications could not be loaded.",
	"nudges.err.update":   "The notification could not be updated.",
	"nudges.err.notfound": "Not found.",
	"nudges.err.failed":   "Something went wrong.",
}
