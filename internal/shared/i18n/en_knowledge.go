package i18n

// englishKnowledge is the knowledge library, a document's page, and passage
// search. File-type names (PDF, Markdown, CSV) are not copy and stay as they
// are in every language.
var englishKnowledge = map[string]string{
	"knowledge.title": "Knowledge",
	"knowledge.intro": "Notes and files the coach can quote. It only uses what it has read here, and it says where each answer came from.",

	// The index meter under the header.
	"knowledge.meter.read":       "read",
	"knowledge.meter.passages":   "passages",
	"knowledge.meter.waiting":    "%[1]d waiting",
	"knowledge.meter.unreadable": "%[1]d unreadable",
	"knowledge.meter.label":      "%[1]d read, %[2]d waiting, %[3]d unreadable",
	"knowledge.rebuild":          "Rebuild index",

	// The last indexing pass, in the terms a person would use.
	"knowledge.run.running": "Indexing is running now.",
	"knowledge.run.failed":  "Last checked %[1]s — %[2]d of %[3]d could not be read.",
	"knowledge.run.nothing": "Last checked %[1]s — nothing to read.",
	"knowledge.run.current": "Last checked %[1]s — everything was already up to date.",
	"knowledge.run.read":    "Last checked %[1]s — read %[2]d of %[3]d.",

	"knowledge.filter.all":     "All",
	"knowledge.filter.notes":   "Notes",
	"knowledge.filter.files":   "Files",
	"knowledge.filter.waiting": "Waiting",

	"knowledge.attention.title":    "Needs attention",
	"knowledge.attention.gap.one":  "1 passage is searchable by word but not yet by meaning.",
	"knowledge.attention.gap.many": "%[1]d passages are searchable by word but not yet by meaning.",
	"knowledge.attention.embed":    "Catch up embeddings",
	"knowledge.read_again":         "Read again",
	"knowledge.remove":             "Remove",

	"knowledge.status.read":     "read",
	"knowledge.status.waiting":  "waiting",
	"knowledge.status.failed":   "could not read",
	"knowledge.status.empty":    "nothing in it",
	"knowledge.status.stale":    "changed since read",
	"knowledge.status.stuck":    "still waiting",
	"knowledge.status.outdated": "read an older way",

	// The compose rail.
	"knowledge.add.title":      "Add to the library",
	"knowledge.add.formats":    "PDF, Markdown, or plain text, up to 8 MB.",
	"knowledge.add.note_label": "Or write a note",
	"knowledge.add.title_ph":   "e.g. Physio notes, March",
	"knowledge.add.body_ph":    "Markdown headings help Khepri quote the right part.",
	"knowledge.add.save":       "Save note",
	"knowledge.drop.title":     "Drop a file, or browse",
	"knowledge.drop.formats":   "PDF · Markdown · text",

	"knowledge.empty.title": "The coach has nothing to quote yet",
	"knowledge.empty.body":  "Add a note, or drop a PDF, Markdown, or text file. Khepri only uses what it has read here.",
	"knowledge.empty.cta":   "Write a note",

	// Library table columns, and the document page's facts.
	"knowledge.col.type":    "Type",
	"knowledge.col.kind":    "Kind",
	"knowledge.col.name":    "Name",
	"knowledge.col.size":    "Size",
	"knowledge.col.lines":   "Lines",
	"knowledge.col.status":  "Status",
	"knowledge.col.read":    "Read",
	"knowledge.col.actions": "Actions",

	"knowledge.type.note": "note",
	"knowledge.type.text": "text",
	// The four-letter mark in the type column; PDF, MD, CSV and TXT are
	// format codes and stay as they are.
	"knowledge.type.code.note": "NOTE",

	"knowledge.detail.upload": "uploaded file",
	"knowledge.detail.lines":  "%[1]d lines",
	"knowledge.detail.read":   "read %[1]s",

	"knowledge.export.body": "Take everything Khepri knows about you — Markdown and original files.",
	"knowledge.export.cta":  "Download export",

	// One document.
	"knowledge.doc.showing":  "Showing lines %[1]d–%[2]d, the passage that was quoted.",
	"knowledge.doc.reread":   "Read it again",
	"knowledge.doc.contents": "Contents",

	// Passage results, inline and behind a reply's sources.
	"knowledge.results.none_inline": "Nothing matched. Khepri can only find what it has read — check the list below for anything still waiting.",
	"knowledge.results.all":         "See all passages",
	"knowledge.results.gone":        "The documents behind this reply are no longer in your knowledge.",
	"knowledge.sources.one":         "1 source",
	"knowledge.sources.many":        "%[1]d sources",

	// The dedicated search page.
	"knowledge.search.title":      "Search knowledge",
	"knowledge.search.heading":    "Search passages",
	"knowledge.search.intro":      "Find what Khepri has read across your notes and files.",
	"knowledge.search.ph":         "Search passages…",
	"knowledge.search.aria":       "Search what Khepri has read",
	"knowledge.search.searching":  "Searching…",
	"knowledge.search.prompt":     "Type a phrase to search what Khepri has read.",
	"knowledge.search.none":       "Nothing matched. Khepri can only find what it has read.",
	"knowledge.search.more":       "Show more",
	"knowledge.search.count.one":  "1 passage",
	"knowledge.search.count.many": "%[1]d passages",
	"knowledge.search.count.more": "%[1]d+ passages",
}
