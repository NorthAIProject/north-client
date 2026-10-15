package i18n

// englishDecisions is the decision journal: the list, the log form, and one
// decision's page. What a person wrote about a decision is never copy.
var englishDecisions = map[string]string{
	"decisions.title":  "Decisions",
	"decisions.label":  "Mind",
	"decisions.intro":  "What you chose, what you turned down, and why.",
	"decisions.empty":  "Nothing recorded yet. The first one is the one you'll want later.",
	"decisions.recent": "Recent",
	"decisions.edit":   "Edit",
	"decisions.delete": "Delete",
	"decisions.back":   "Back to decisions",

	"decisions.form.new":          "Log a decision",
	"decisions.form.save":         "Save decision",
	"decisions.form.save_changes": "Save changes",
	"decisions.form.title":        "The decision",
	"decisions.form.title_ph":     "e.g. Quit the evening client",
	"decisions.form.options":      "Options considered",
	"decisions.form.options_ph":   "What else was on the table?",
	"decisions.form.rationale":    "Why this one",
	"decisions.form.rationale_ph": "What tipped it?",
	"decisions.form.outcome":      "How it turned out",
	"decisions.form.outcome_ph":   "Looking back — what actually happened?",
	"decisions.form.held":         "Did it hold?",

	// Answers to "did it hold?", by the stored value.
	"decisions.held.none":   "Not yet",
	"decisions.held.yes":    "Yes",
	"decisions.held.partly": "Partly",
	"decisions.held.no":     "No",

	// One decision in the list. The argument is the person's own text.
	"decisions.card.options": "Options: %[1]s",
	"decisions.card.why":     "Why: %[1]s",
	"decisions.card.outcome": "Outcome: %[1]s",
	"decisions.card.held":    "Held: %[1]s",
}
