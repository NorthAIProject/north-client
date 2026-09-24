package i18n

// englishDictate is the microphone beside every AI-facing text box, and the
// replies of the endpoint behind it.
//
// The client-side strings reach dictate.js on data- attributes of the button,
// so that script holds no English of its own. The server-side ones are the
// error messages POST /app/voice/transcribe answers with, which the button
// shows as they arrive.
var englishDictate = map[string]string{
	"dictate.aria":        "Dictate instead of typing",
	"dictate.say":         "Say it",
	"dictate.stop":        "Stop",
	"dictate.reading":     "Reading it…",
	"dictate.empty":       "That recording was empty.",
	"dictate.toolong":     "That recording is too long.",
	"dictate.unreachable": "That did not reach Khepri. Try again.",
	"dictate.unreadable":  "Khepri could not read that recording. Try again.",
	"dictate.mic":         "Khepri could not reach the microphone. Check the permission and try again.",
	"dictate.unsupported": "This browser cannot record audio.",
	"dictate.limit":       "You have dictated a lot for now. Type it instead, or try again later.",
	"dictate.unavailable": "I cannot listen to recordings right now. Type it instead and everything else works as usual.",
	"dictate.silent":      "I could not hear anything in that.",
	"dictate.failed":      "Something went wrong listening to that. Try again.",
}
