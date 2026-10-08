package planimport

import (
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/planimport/draft"
)

// The draft types live in their own package so the review pages can render
// them without importing this one. Aliased here, as workouts aliases plan.
type (
	WorkoutDraft    = draft.WorkoutDraft
	WorkoutDayDraft = draft.WorkoutDayDraft
	ExerciseDraft   = draft.ExerciseDraft
	MealDraft       = draft.MealDraft
	MealDayDraft    = draft.MealDayDraft
	MealDraftMeal   = draft.MealDraftMeal
	FoodDraft       = draft.FoodDraft
	Candidate       = draft.Candidate
	MacroGrams      = draft.MacroGrams
)

func macroGrams(m meal.Macros) *MacroGrams {
	return &MacroGrams{Calories: m.Calories, ProteinG: m.ProteinG, CarbG: m.CarbG, FatG: m.FatG}
}
