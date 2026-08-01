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
)

var (
	PlanSchema = plan.PlanSchema
	Validate   = plan.Validate
)
