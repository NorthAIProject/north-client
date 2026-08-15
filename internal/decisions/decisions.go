// Package decisions owns the decision journal: recorded calls, the options
// that were on the table, and putting the relevant ones in front of the coach.
//
// Distinct from internal/mind's journal (free-form reflection) and from goals
// (standing intentions). A decision is a log of a choice, not a feeling and
// not a target.
package decisions

import "github.com/NorthAIProject/north-client/internal/decisions/decision"

type Decision = decision.Decision
