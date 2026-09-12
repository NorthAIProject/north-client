// Package score turns a window of logged data into a 0-100 reading per life
// domain, the way a watch turns a night's sleep into one number.
//
// A leaf: pure functions over shapes the insights service already loads, with
// no I/O and no imports from the slices it scores. That is what lets the web
// page and a Telegram digest produce byte-identical numbers, and it is why the
// whole package is table-testable without a database.
//
// Nothing here calls a model. A score that costs a generation could not be
// shown on every page load, and a number that changes between two renders of
// the same window is not a measurement.
package score

// Component is one weighted part of a domain score.
//
// Weight is the points available, Earned the points taken. Both are plain
// integers so the breakdown can be shown the way a watch shows it —
// "Duration 46/50" — rather than as a percentage of a percentage.
type Component struct {
	// Key is an i18n key suffix, never display text. The view layer renders
	// it; a sentence assembled here could not be translated.
	Key string

	Earned int
	Weight int

	// Known is false when the window holds no measurement for this component.
	// An unknown component is dropped from the score rather than counted as
	// zero: somebody who did not log a bedtime did not sleep badly.
	Known bool
}

// minCoverage is how much of a domain's weight must be measured before the
// score is worth showing. Below it, one logged glass of water would render as
// a confident number about a person's whole body.
const minCoverage = 40

// Score is one domain's reading over a window.
type Score struct {
	Domain string

	// Points is 0-100, renormalised over the known components alone.
	Points int

	Components []Component

	// Coverage is the share of the domain's weight that was measured.
	Coverage int

	// HasData reports whether Points means anything. False renders as "not
	// enough data", which is the honest answer and the one Apple gives.
	HasData bool
}

// New computes a domain score from its components.
func New(domain string, components []Component) Score {
	var earned, weight int
	for _, c := range components {
		if !c.Known {
			continue
		}
		earned += c.Earned
		weight += c.Weight
	}

	out := Score{
		Domain:     domain,
		Components: components,
		Coverage:   weight,
		HasData:    weight >= minCoverage,
	}
	if out.HasData {
		out.Points = earned * 100 / weight
	}
	return out
}
