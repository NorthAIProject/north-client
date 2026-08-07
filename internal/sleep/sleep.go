// Package sleep tracks how long and how well a person slept.
//
// One row per night, corrected in place rather than appended to: you did not
// have two of last night, and the common edit is fixing an estimate the next
// morning.
//
// Sleep earns its own slice rather than becoming two more check-in fields
// because it is recorded at a different moment (on waking, not on reflecting)
// and is the signal most likely to explain a bad week in every other domain.
package sleep

import "github.com/NorthAIProject/north-client/internal/sleep/sleep"

// The night shapes live in a leaf package so the service and any template
// that renders them do not import each other.
type (
	Log   = sleep.Log
	Trend = sleep.Trend
)
