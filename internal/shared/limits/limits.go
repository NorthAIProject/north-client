// Package limits holds the bounds every surface applies to list-style requests.
//
// One module rather than a constant per caller, because the same search runs
// behind the web handler, the coach's context builder, and an MCP tool held by
// an agent — and a bound that only one of them enforces is not a bound. The MCP
// surface in particular is reachable by a bearer token with no per-caller
// identity, so "the UI would never send that" is not an argument there.
package limits

import "fmt"

const (
	// MinLimit is the smallest row count any list-style request may ask for.
	MinLimit = 1

	// MaxSearchLimit bounds a search. It is small because search results are
	// spent on a context window, not rendered as a page: fifty retrieved facts
	// already crowd out the conversation they were meant to inform.
	MaxSearchLimit = 50

	// MaxBrowseLimit bounds a plain listing, which is paginated for a human and
	// can afford to be larger.
	MaxBrowseLimit = 200

	// MaxRecentLimit bounds a newest-first listing.
	MaxRecentLimit = 100

	// MaxSearchTermLength caps a query before it reaches Postgres. A caller
	// cannot be allowed to hand the parser an unbounded string and make the
	// database do the rejecting.
	MaxSearchTermLength = 1000
)

// Validate returns value when it lies within [MinLimit, max].
//
// Rejects rather than clamps: a caller that asked for 10,000 rows and silently
// received 50 has been told something false about what it is holding.
func Validate(value int, name string, max int) (int, error) {
	if value < MinLimit || value > max {
		return 0, fmt.Errorf("%s must be between %d and %d, got %d", name, MinLimit, max, value)
	}
	return value, nil
}
