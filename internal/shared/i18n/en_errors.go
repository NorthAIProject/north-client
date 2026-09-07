package i18n

// englishErrors is the field-validation copy. These reach a person at the worst
// moment — something they typed was refused — which is exactly when falling
// back to a language they did not choose is least forgivable.
var englishErrors = map[string]string{
	"users.err.email.required": "Email is required.",
	"users.err.email.invalid":  "That does not look like a valid email address.",
	"users.err.name.required":  "Name is required.",
	"users.err.name.toolong":   "Name must be 100 characters or fewer.",
	"users.err.tone.invalid":   "Choose one of the listed tones.",
	"users.err.style.toolong":  "Keep this under 1000 characters.",
}
