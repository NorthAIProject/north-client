package i18n

// englishAuth is the signed-out pages: sign in, sign up, and the password
// reset pair. Form-level errors come from internal/auth and are not here.
var englishAuth = map[string]string{
	"auth.field.email":       "Email",
	"auth.field.password":    "Password",
	"auth.field.name":        "Name",
	"auth.field.confirm":     "Confirm password",
	"auth.field.newpassword": "New password",
	"auth.password.hint":     "At least 12 characters. A short sentence works well.",
	"auth.back":              "Back to sign in",

	"auth.alt.google":  "Continue with Google",
	"auth.alt.passkey": "Use a passkey",
	"auth.alt.or":      "or",

	"auth.signin.page":      "Sign in",
	"auth.signin.title":     "Welcome back",
	"auth.signin.desc":      "Sign in to continue with your coach.",
	"auth.signin.forgot":    "Forgot password?",
	"auth.signin.submit":    "Sign in",
	"auth.signin.noaccount": "No account?",
	"auth.signin.create":    "Create one",

	"auth.signup.title":      "Create your account",
	"auth.signup.desc":       "Khepri remembers your goals and your progress. Start by telling it who you are.",
	"auth.signup.passkey":    "Create account with a passkey",
	"auth.signup.orpassword": "or with password",
	"auth.signup.submit":     "Create account",
	"auth.signup.have":       "Already have an account?",

	// The consent notice is split around its two links. Each piece is a
	// fragment of one sentence, so translate them together.
	"auth.consent.agree":   "By creating an account you agree to the",
	"auth.consent.terms":   "terms",
	"auth.consent.and":     "and the",
	"auth.consent.privacy": "privacy policy",
	"auth.consent.health":  ", and you explicitly consent to Khepri storing and processing the health data you choose to record — weight, sleep, activity and anything health-related you write. You can export or delete all of it at any time.",

	"auth.forgot.page":       "Reset your password",
	"auth.forgot.title":      "Forgot your password?",
	"auth.forgot.sent":       "If an account exists for that address, we sent a reset link.",
	"auth.forgot.desc":       "Enter the email you use with Khepri and we will send a link to choose a new password.",
	"auth.forgot.check":      "Check your inbox for a message from Khepri. The link expires in one hour. If nothing arrives, check spam or try again in a few minutes.",
	"auth.forgot.submit":     "Send reset link",
	"auth.forgot.remembered": "Remembered it?",

	"auth.reset.title":  "Choose a new password",
	"auth.reset.desc":   "Pick something you will remember. At least 12 characters — a short sentence works well.",
	"auth.reset.again":  "Request a new link",
	"auth.reset.submit": "Update password",
}
