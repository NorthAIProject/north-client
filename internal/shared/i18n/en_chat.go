package i18n

// englishChat is the coach conversation page.
var englishChat = map[string]string{
	"chat.empty.mono":    "Day 1 · no sessions yet",
	"chat.empty.heading": "Talk to your coach",
	"chat.empty.body":    "Khepri remembers what you tell it. Start with what you are working on.",

	"chat.new":              "New conversation",
	"chat.reflection":       "Reflection",
	"chat.reflection.start": "Start a reflection",
	"chat.threads.aria":     "Show conversations",
	"chat.header.new":       "New chat",
	"chat.status.ready":     "Ready",
	"chat.delete.aria":      "Delete conversation",
	"chat.delete.confirm":   "Delete this conversation? Everything in it is lost.",
	"chat.kpi.threads":      "Threads",
	"chat.kpi.messages":     "Messages",

	"chat.thread.mono":    "This thread",
	"chat.thread.heading": "What's on your mind?",
	"chat.thread.body":    "Tell Khepri what you are working on. It remembers, so you only have to explain once.",

	// Conversation starters. A blank box is the hardest thing to answer, and
	// these say what Khepri is for better than a paragraph would — so they are
	// written for each language rather than translated word for word.
	"chat.starter.1": "I want to get stronger but I don't know where to start.",
	"chat.starter.2": "Help me build a habit I'll actually keep.",
	"chat.starter.3": "I've been training for a month and feel stuck.",
	"chat.starter.4": "I keep starting things and not finishing them.",

	"chat.feedback.helpful":    "Marked helpful",
	"chat.feedback.nothelpful": "Marked not helpful",
	"chat.feedback.undo":       "Undo",
	"chat.feedback.ask":        "Did this help?",
	"chat.feedback.yes":        "Yes",
	"chat.feedback.no":         "No",

	"chat.attach":      "Attach a photo",
	"chat.placeholder": "What are you working on?",

	"chat.approval.title": "Khepri wants to change something",
	"chat.approval.yes":   "Yes, go ahead",
	"chat.approval.no":    "No",

	// The Muse header's status line under the name pill
	// (_reviews/muse-chat-contract.md). Each reads after "Khepri", so they
	// are verb phrases, not sentences.
	"chat.status.listening": "is listening",
	"chat.status.thinking":  "is thinking",
	"chat.status.writing":   "is writing",
	"chat.status.snag":      "hit a snag",
	"chat.status.tool":      "is %[1]s",

	// What a running tool is doing, slotted into chat.status.tool. The
	// contract caps each at 32 characters; TestToolStatusLabelsFitTheHeader
	// holds every language to it.
	"chat.tool.default":       "looking things up",
	"chat.tool.exercises":     "looking up exercises",
	"chat.tool.macros":        "working out your macros",
	"chat.tool.goals":         "checking your goals",
	"chat.tool.goals.write":   "updating your goals",
	"chat.tool.checkin":       "logging your check-in",
	"chat.tool.checkin.read":  "reading your check-ins",
	"chat.tool.documents":     "searching your documents",
	"chat.tool.workout":       "reading your workout plan",
	"chat.tool.workout.write": "editing your workout plan",
	"chat.tool.nutrition":     "checking your nutrition",
	"chat.tool.alerts":        "checking your reminders",
	"chat.tool.alerts.write":  "setting a reminder",
	"chat.tool.log":           "logging that for you",

	// Captions above a proactive bubble (_reviews/muse-chat-contract.md,
	// "Proactive messages"). Rendered uppercase by CSS, written in sentence
	// case here so screen readers do not spell them out.
	"chat.caption.briefing":  "Briefing · %[1]s",
	"chat.caption.standing":  "Standing task",
	"chat.caption.from":      "From %[1]s",
	"chat.caption.proactive": "Unprompted",

	// The standing-task card (create_watch).
	"chat.watch.daily":   "Every day at %[1]s",
	"chat.watch.weekly":  "Every %[1]s at %[2]s",
	"chat.watch.when":    "Pings you when %[1]s",
	"chat.watch.confirm": "Confirm",
	"chat.watch.notnow":  "Not now",
	"chat.watch.day.0":   "Sunday",
	"chat.watch.day.1":   "Monday",
	"chat.watch.day.2":   "Tuesday",
	"chat.watch.day.3":   "Wednesday",
	"chat.watch.day.4":   "Thursday",
	"chat.watch.day.5":   "Friday",
	"chat.watch.day.6":   "Saturday",
}
