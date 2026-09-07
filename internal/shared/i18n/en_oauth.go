package i18n

// englishOAuth is the agent consent screen: the page a person lands on when
// they paste Khepri's MCP URL into a client.
//
// Its own surface because it is the only page that is simultaneously an
// advertisement, a signup form and an install step. The copy is written for
// somebody who has never heard of North and arrived here from a tool they
// already trust — which is why it names the destination in plain words rather
// than in scope strings.
var englishOAuth = map[string]string{
	"oauth.title": "Connect an agent",

	// The client's own name is interpolated. It is chosen by whoever
	// registered the client, so the line beneath it — the host the access
	// actually goes to — is what a person should be reading.
	"oauth.asking":            "%[1]s wants to connect to your Khepri account.",
	"oauth.destination":       "It will send your access to %[1]s.",
	"oauth.destination.local": "It will send your access to this computer.",

	"oauth.account.signed-in": "Signed in as %[1]s",
	"oauth.account.notyou":    "Not you?",

	"oauth.can.title":         "What it will be able to do",
	"oauth.can.read":          "Read your goals, check-ins, training, remembered facts and documents.",
	"oauth.can.write":         "Log check-ins, complete habits, record your weight, log food, and edit your training plan.",
	"oauth.can.readonly.note": "It will not be able to change anything.",

	"oauth.readonly.label": "Read-only",
	"oauth.readonly.hint":  "Let it read your data without changing anything.",

	"oauth.approve":     "Approve",
	"oauth.deny":        "Cancel",
	"oauth.revoke.note": "You can revoke this any time in Settings → Connections.",

	// The account block, for somebody with no Khepri account yet. This is the
	// signup, and it is deliberately four fields and no questionnaire.
	"oauth.new.title":    "Create your Khepri account",
	"oauth.new.desc":     "You are about to give an agent a memory that lasts. It needs somewhere to keep it.",
	"oauth.new.name":     "Your name",
	"oauth.new.email":    "Email",
	"oauth.new.password": "Password",
	"oauth.new.submit":   "Create account and continue",
	"oauth.new.haveone":  "Already have an account?",

	"oauth.signin.title":  "Sign in to continue",
	"oauth.signin.submit": "Sign in",
	"oauth.signin.new":    "Need an account?",

	"oauth.or":      "or",
	"oauth.google":  "Continue with Google",
	"oauth.passkey": "Continue with a passkey",

	// Errors. The fatal ones render a page rather than redirecting, because
	// the destination is not trustworthy at that point.
	"oauth.error.title":          "That request cannot be completed",
	"oauth.error.unknown-client": "The application making this request is not registered with Khepri.",
	"oauth.error.bad-redirect":   "This request asked to send your access somewhere the application has not registered.",
	"oauth.error.no-client":      "This request did not say which application is asking.",
	"oauth.error.expired":        "This request has expired. Start again from your agent.",
	"oauth.error.home":           "Go to Khepri",
}
