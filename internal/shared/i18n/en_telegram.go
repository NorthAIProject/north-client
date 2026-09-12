package i18n

// englishTelegram is what the bot says on its own account, as opposed to what
// the coach says through it.
//
// The coach's replies are already in the user's language — coach_language.md
// puts that in the system prompt. These are the adapter's own words: the
// approval prompt, the quota refusals, the link and unlink replies. Left in
// English they were the seam where a Portuguese conversation switched language
// mid-thread, and the approval prompt is the worst possible place for that,
// since it is the consent gate before Khepri writes anything.
var englishTelegram = map[string]string{
	"tg.start":             "You are linked to %[1]s. Ask me anything you would ask in the web app — I have the same memory, goals and check-ins.\n\nSend /help to see what I can do.",
	"tg.help":              "Ask me anything you would ask in the web app — how a goal is going, what your week looked like, whether to train today.\n\nThis is the same conversation as the web chat. Ask here, and the answer is there too.\n\nBefore I write anything down — a check-in, a goal — I will show you what I am about to do and wait for a yes.\n\n/help — this message\n/stats — how things are going (add week, month or year)\n/unlink — disconnect this chat from your account",
	"tg.notlinked":         "This chat is not linked to a Khepri account.",
	"tg.stats.unavailable": "I cannot read your numbers on this server yet. They are all in the web app.",
	"tg.stats.failed":      "I could not read your numbers just now. Try again in a moment.",
	"tg.stats.empty":       "Nothing logged in that window yet.",
	"tg.unlinked":          "Disconnected. Nothing you have said is deleted — the whole conversation is still in the web app. To connect again, get a new code from Settings → Agent connections.",

	"tg.onboard":     "Finish setting up your account in the Khepri web app first, then message me again.",
	"tg.linked":      "Linked to %[1]s. Message me whenever — I have the same memory and goals as the web app.",
	"tg.takenlink":   "This chat is already linked to another Khepri account.",
	"tg.toomany":     "Too many attempts. Wait a minute and try your code again.",
	"tg.photofailed": "That photo could not be stored.",

	// Voice notes. Each one names what actually happened, because "something
	// went wrong" after somebody has spoken a sentence tells them nothing about
	// whether saying it again would work.
	"tg.voice.unavailable": "I cannot listen to voice notes on this server yet. Type it and I will answer the same way.",
	"tg.voice.toolong":     "That voice note is longer than I can listen to. Try a shorter one, or type it.",
	"tg.voice.toobig":      "That recording is bigger than I can handle. Try a shorter one.",
	"tg.voice.failed":      "I could not make out that voice note. Try again, or type it.",
	"tg.voice.silent":      "I did not hear anything in that. Try again?",
	"tg.voice.download":    "I could not download that voice note. Try sending it again?",
	"tg.wrong":             "Something went wrong on my side. Try again in a moment.",

	// The consent gate. Asking permission in a language somebody did not choose
	// is the one place where falling back to English is not merely untidy.
	"tg.confirm.again": "I still need a yes or a no first.",
	"tg.confirm":       "Before I do this, can you confirm?",
	"tg.confirm.yes":   "Yes, do it",
	"tg.confirm.no":    "No",

	"tg.quota.minute": "You have reached your coach message limit. Try again in less than a minute.",
	"tg.quota.hour":   "You have reached your coach message limit. Try again in about an hour.",
	"tg.quota.n":      "You have reached your coach message limit. Try again in %[1]d minutes.",
}
