package i18n

// englishMemories is the long-term memory page, and the "save anything from
// this reflection?" chips a finished reflection loads. Fact contents are the
// person's own words and never pass through here.
var englishMemories = map[string]string{
	"memories.title": "Memory",
	"memories.label": "Long-term",
	"memories.intro": "Facts Khepri may use in coaching. Proposed facts from conversations wait here until you approve them.",

	"memories.kpi.pending":  "Pending",
	"memories.kpi.approved": "Approved",
	"memories.kpi.pinned":   "Pinned",
	"memories.kpi.none":     "None",

	"memories.donut.title": "Facts by category",
	"memories.donut.empty": "No approved facts yet.",

	"memories.proposed.title": "Proposed from conversations",
	"memories.known.title":    "Known facts",
	"memories.known.empty":    "Nothing stored yet. Approve a proposal or add a fact.",

	"memories.add.title":  "Add a fact",
	"memories.add.ph":     "e.g. Prefers morning training before 8am",
	"memories.add.save":   "Save fact",
	"memories.fact":       "Fact",
	"memories.category":   "Category",
	"memories.choose_one": "Choose one",
	"memories.fix.title":  "Fix this fact",

	"memories.badge.proposed":   "proposed",
	"memories.badge.replaces":   "replaces a known fact",
	"memories.badge.pinned":     "pinned",
	"memories.badge.hidden":     "hidden from coach",
	"memories.badge.superseded": "no longer current",

	"memories.replaces.body":   "Approving this retires an existing fact, so the coach stops being told the older one. A pinned fact is never retired this way.",
	"memories.superseded.body": "A newer fact replaced this one. It is kept as history and is not shown to the coach.",

	"memories.approve": "Approve",
	"memories.discard": "Discard",
	"memories.edit":    "Edit",
	"memories.save":    "Save",
	"memories.cancel":  "Cancel",
	"memories.pin":     "Pin",
	"memories.unpin":   "Unpin",
	"memories.hide":    "Hide from coach",
	"memories.include": "Use in coaching",
	"memories.delete":  "Delete",

	"memories.delete.title": "Delete this fact?",
	"memories.delete.body":  "Khepri will stop remembering it. This cannot be undone from here.",
	"memories.delete.keep":  "Keep it",

	"memories.reflection.ask":  "Save anything from this reflection?",
	"memories.reflection.save": "Save to memory",
	"memories.reflection.skip": "Skip",

	// Category labels, by the value memory.Categories stores. Lower case, as
	// the stored value always rendered: headings uppercase it with CSS.
	"memories.category.preference": "preference",
	"memories.category.constraint": "constraint",
	"memories.category.habit":      "habit",
	"memories.category.injury":     "injury",
	"memories.category.equipment":  "equipment",
	"memories.category.schedule":   "schedule",
	"memories.category.coaching":   "coaching",
	"memories.category.general":    "general",
}
