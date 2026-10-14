package i18n

// englishForm is the form check: uploading a clip and reading the analysis.
var englishForm = map[string]string{
	"form.title":         "Form check",
	"form.lede":          "Upload a clip of a working set. Khepri watches it and points at the moments worth fixing — you can see each one for yourself.",
	"form.upload.help":   "MP4, MOV, or WebM, under 200 MB. Film from the side, full body in frame, one set.",
	"form.upload.submit": "Analyse my form",
	"form.previous":      "Previous checks",
	"form.new":           "New check",

	"form.badge.clean":   "Looks clean",
	"form.badge.unclear": "Unclear footage",
	"form.badge.tofix":   "%[1]d to fix",
	"form.badge.clear":   "Clear footage",
	"form.badge.mostly":  "Mostly clear",

	"form.status.watching":    "Khepri is watching your set. This usually takes under a minute.",
	"form.status.failed":      "That did not work",
	"form.status.failed.desc": "Something went wrong analysing that video.",

	"form.result.title":       "What Khepri saw",
	"form.result.unclear":     "Khepri could not see the movement clearly enough to judge it. Try filming from the side, further back, with your whole body in frame.",
	"form.result.clean":       "Nothing worth changing in this set. Keep the load where it is and film again if it starts to feel different.",
	"form.result.issues.one":  "1 thing to work on",
	"form.result.issues.many": "%[1]d things to work on",
	"form.result.tap":         "tap a timestamp to jump there",

	"form.severity.critical": "Fix before adding load",
	"form.severity.moderate": "Worth fixing",
	"form.severity.minor":    "Minor",
}
