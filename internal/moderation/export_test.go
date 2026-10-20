package moderation

// TermsForTest are the embedded lists by locale, so tests can use their
// entries without spelling any of them out.
func TermsForTest() map[string][]string { return loadLists() }
