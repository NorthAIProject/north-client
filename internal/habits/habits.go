// Package habits tracks recurring intentions and how well they are kept.
//
// The distinction that matters against the log slices (hydration, sleep,
// activity): a log records something that happened and can never be wrong,
// while a habit carries an expectation and therefore can be missed. Streaks
// and adherence only mean anything against an expectation, which is why this
// is a separate concept rather than a flag on a log.
package habits

import "github.com/NorthAIProject/north-client/internal/habits/habit"

// The habit shapes and the adherence maths live in a leaf package so the
// service and any template that renders a habit do not import each other.
type (
	Habit      = habit.Habit
	Completion = habit.Completion
	Stats      = habit.Stats
)
