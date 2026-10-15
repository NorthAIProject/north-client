package i18n

// englishQuota is the panel shown when a rate limit refuses a request. The
// title takes an action name, the body a wait ("in 47 minutes").
var englishQuota = map[string]string{
	"quota.title": "You have hit the limit on %[1]s",
	"quota.body":  "Nothing was lost. Try again %[1]s.",

	"quota.action.coach_message":    "coach messages",
	"quota.action.document_upload":  "uploads",
	"quota.action.document_reindex": "reindexing",
	"quota.action.report_generate":  "report generation",
	"quota.action.media_analysis":   "video analysis",
	"quota.action.account_export":   "account exports",
	"quota.action.quick_capture":    "quick capture",
	"quota.action.voice_capture":    "voice notes",
	"quota.action.plan_import":      "plan imports",

	"quota.delay.underminute": "in less than a minute",
	"quota.delay.minute":      "in about a minute",
	"quota.delay.minutes":     "in %[1]d minutes",
	"quota.delay.hour":        "in about an hour",
	"quota.delay.hours":       "in %[1]d hours",

	// The API's 429 message: an action name, then a wait.
	"quota.api": "You have used all your %[1]s for now. Try again %[2]s.",
}
