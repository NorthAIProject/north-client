package i18n

// englishMoments is the recognition card a check-in streak, a goal or a checkpoint earns.
var englishMoments = map[string]string{
	// Check-in streaks earn a card at these lengths only; see internal/moments.
	"moments.streak.3.title":   "Three days running",
	"moments.streak.3.body":    "That is a habit starting. Same time tomorrow.",
	"moments.streak.7.title":   "A full week",
	"moments.streak.7.body":    "Seven check-ins in a row. Khepri has a real picture of you now.",
	"moments.streak.14.title":  "Two weeks",
	"moments.streak.14.body":   "The reviews get sharper from here. Keep feeding them.",
	"moments.streak.30.title":  "Thirty days",
	"moments.streak.30.body":   "A month of showing up. Most people never get here.",
	"moments.streak.60.title":  "Sixty days",
	"moments.streak.60.body":   "Two months. This is who you are now, not something you are trying.",
	"moments.streak.100.title": "One hundred days",
	"moments.streak.100.body":  "A hundred check-ins. Khepri remembers every one.",

	// The goal title, as the person wrote it.
	"moments.goal.title":      "“%[1]s” — achieved",
	"moments.goal.body":       "You named it, you did it. Khepri will remember this one.",
	"moments.milestone.title": "Checkpoint reached",
	// The checkpoint, then the goal it leads to.
	"moments.milestone.body": "“%[1]s”, on the way to “%[2]s”.",
}
