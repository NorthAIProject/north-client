// Package workoutsummary renders a finished workout, a plan's week and the
// muscle map — the cards the plan page and Insights → Training both show.
//
// A shared package rather than one page importing the other's templates, for
// the reason web/shared/exerciserow gives: web/workouts owns the plan page,
// web/insights owns Insights, and neither should reach into the other. The
// views the cards draw are built in internal/workouts/workoutsummary.
package workoutsummary
