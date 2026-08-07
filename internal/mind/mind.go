// Package mind owns North's mood/reflection view: a trend built from
// check-ins' existing mood/energy fields, plus free-form journal entries
// that are new to this package. It does not duplicate check-in's structured
// fields — this is a place for the parts a 1-5 scale can't capture.
package mind

import "github.com/NorthAIProject/north-client/internal/mind/journal"

type JournalEntry = journal.Entry
