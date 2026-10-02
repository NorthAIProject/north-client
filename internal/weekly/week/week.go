// Package week holds the closed weekly loop's types: what someone chose to
// focus on for a week, and the review that asks them.
package week

import (
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// MaxPriorities is how many priorities a week can have. More than three is a
// list, not a focus.
const MaxPriorities = 3

// Focus is what someone chose for one week, Monday to Sunday in their time
// zone.
type Focus struct {
	WeekStart  time.Time
	Priorities []string
	Volume     plan.Volume
	ReviewedAt time.Time
}

// Goal is an active goal as the review shows it, in the person's order.
type Goal struct {
	ID       uuid.UUID
	Title    string
	Category string
	Priority int
}

// Report is the weekly report about the week being reviewed, when there is
// one. Ready is false while it is still being written.
type Report struct {
	ID    uuid.UUID
	Title string
	Body  string
	Ready bool
}

// Review is everything the weekly review needs on one screen: the week that
// happened, what was chosen for it, and the week being planned.
type Review struct {
	// Reviewing is the Monday of the week that happened; Planning is the
	// Monday of the week the new focus is for.
	Reviewing time.Time
	Planning  time.Time

	Report *Report
	// Last is the focus chosen for the week that happened, so the review can
	// ask whether it held.
	Last *Focus
	// Current is a focus already set for the week being planned, when the
	// review is being done again.
	Current *Focus
	Goals   []Goal
}

// Input is what the person submits at the end of a review.
type Input struct {
	Priorities []string
	// GoalOrder is their active goals, first first.
	GoalOrder []uuid.UUID
	Volume    plan.Volume
}
