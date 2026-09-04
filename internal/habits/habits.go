// Package habits tracks recurring intentions and how well they are kept.
//
// The distinction that matters against the log slices (hydration, sleep,
// activity): a log records something that happened and can never be wrong,
// while a habit carries an expectation and therefore can be missed. Streaks
// and adherence only mean anything against an expectation, which is why this
// is a separate concept rather than a flag on a log.
package habits

import (
	"strings"

	"github.com/NorthAIProject/north-client/internal/habits/habit"
)

// The habit shapes and the adherence maths live in a leaf package so the
// service and any template that renders a habit do not import each other.
type (
	Habit      = habit.Habit
	Completion = habit.Completion
	Stats      = habit.Stats
)

// Match finds the one habit a spoken name refers to.
//
// It lives here rather than in a caller because what a habit's name means is
// the habits slice's business, and it has two callers with different manners:
// the coach refuses an ambiguous name out loud, and quick capture shows it back
// for the person to pick. Both have to agree about what "cold" matches, or the
// same sentence gets two answers depending on where it was typed.
//
// An exact name wins outright, then a unique partial. Anything else is
// reported rather than guessed: ticking off the wrong habit is invisible to the
// person and corrupts a streak they care about.
func Match(list []Habit, name string) (match Habit, candidates []Habit) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" || len(list) == 0 {
		return Habit{}, nil
	}

	var partial []Habit
	for _, h := range list {
		got := strings.ToLower(h.Name)
		if got == want {
			return h, nil
		}
		if strings.Contains(got, want) || strings.Contains(want, got) {
			partial = append(partial, h)
		}
	}

	if len(partial) == 1 {
		return partial[0], nil
	}
	return Habit{}, partial
}
