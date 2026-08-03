// Package goals owns what the user is working toward: creating goals, tracking
// them, and putting the active ones in front of the coach.
package goals

import "github.com/NorthAIProject/north-client/internal/goals/goal"

// The goal shape lives in a leaf package so the templates that render a goal
// and the handler that serves them do not import each other. These aliases mean
// callers still say goals.Goal.
type (
	Goal   = goal.Goal
	Update = goal.Update
)

const (
	StatusActive    = goal.StatusActive
	StatusAchieved  = goal.StatusAchieved
	StatusPaused    = goal.StatusPaused
	StatusAbandoned = goal.StatusAbandoned

	CategoryFitness  = goal.CategoryFitness
	CategoryHealth   = goal.CategoryHealth
	CategoryWork     = goal.CategoryWork
	CategoryLearning = goal.CategoryLearning
	CategoryPersonal = goal.CategoryPersonal
	CategoryOther    = goal.CategoryOther
)

var (
	Categories = goal.Categories
	Statuses   = goal.Statuses
)
