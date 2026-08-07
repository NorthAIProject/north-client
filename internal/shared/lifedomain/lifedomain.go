// Package lifedomain holds the one list of life domains North coaches across.
//
// North started as a training app, so for a while "which part of someone's
// life is this about" only needed answering inside goals. It now needs
// answering in several places at once — habits, goals, and eventually the
// coach's own cross-domain reasoning — and a vocabulary that lives in one
// slice is a vocabulary that drifts the moment a second slice needs it.
//
// So this is deliberately the smallest possible package: a list of strings and
// nothing else. It has no dependencies and imports nothing, which is what lets
// any leaf domain package use it without creating a cycle.
//
// The values are exactly the categories goals has always offered, so adopting
// this changes no stored data. See DOMAIN.md for how domains map onto slices.
package lifedomain

import "slices"

// The domains themselves. Broad on purpose: fine-grained taxonomies make
// people stop and classify instead of write, and North gains nothing from the
// precision. (This reasoning is inherited from goals, which chose these first.)
const (
	Fitness  = "fitness"
	Health   = "health"
	Work     = "work"
	Learning = "learning"
	Personal = "personal"
	Other    = "other"
)

// Domains is the ordered set offered in the UI and accepted on write.
//
// Order is display order, not priority: it runs from the most concrete
// ("fitness") to the least ("other"), which is the order people tend to reach
// for them in.
var Domains = []string{Fitness, Health, Work, Learning, Personal, Other}

// Valid reports whether a domain is one this product understands. Callers
// validate on write rather than constraining the column, matching how goals
// and memories already handle their vocabularies.
func Valid(domain string) bool {
	return slices.Contains(Domains, domain)
}
