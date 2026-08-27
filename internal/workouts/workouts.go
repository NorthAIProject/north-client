// Package workouts turns what someone tells you about their training into a
// validated, stored plan.
package workouts

import "github.com/NorthAIProject/north-client/internal/workouts/plan"

// The plan shape lives in a leaf package so the templates that render a plan
// and the handler that serves them do not import each other. These aliases mean
// callers still say workouts.Plan and never have to know that.
type (
	Plan     = plan.Plan
	PlanDay  = plan.PlanDay
	Exercise = plan.Exercise
	Intake   = plan.Intake

	// Movement is the part of an exercise a swap replaces — the lift itself,
	// not the sets and reps prescribed of it.
	Movement = plan.Movement
)

var (
	PlanSchema = plan.PlanSchema
	Validate   = plan.Validate

	// Swap and its siblings are pure: a plan in, a new plan out. The service
	// wraps them with loading, validation and storage; see applyEdit.
	Swap            = plan.Swap
	Insert          = plan.Insert
	Remove          = plan.Remove
	Move            = plan.Move
	SetPrescription = plan.SetPrescription
	NewExercise     = plan.NewExercise
)
