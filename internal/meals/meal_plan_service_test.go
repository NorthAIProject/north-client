package meals_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func within(got, want, tolerance float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff < tolerance
}

// TestTotalsAlwaysEqualTheSumOfChildren is the most important test in this
// package: meal and plan total_macros are a cache, and a cache that drifts
// from its source is worse than no cache at all.
func TestTotalsAlwaysEqualTheSumOfChildren(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	planSvc := meals.NewMealPlanService(repo)
	ctx := context.Background()

	chicken, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "Chicken breast", Category: meals.CategoryProtein,
		Per100g: meals.Macros{Calories: 165, ProteinG: 31, FatG: 3.6, CarbG: 0},
	})
	if err != nil {
		t.Fatalf("create chicken: %v", err)
	}
	rice, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "White rice", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28},
	})
	if err != nil {
		t.Fatalf("create rice: %v", err)
	}

	plan, err := planSvc.CreatePlan(ctx, user.ID, meals.MealPlanInput{Name: "Cutting plan"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	meal, err := planSvc.AddMeal(ctx, plan.ID, user.ID, meals.MealInput{Name: "Lunch", MealNumber: 1})
	if err != nil {
		t.Fatalf("add meal: %v", err)
	}

	chickenLine, err := planSvc.AddIngredient(ctx, meal.ID, user.ID, meals.MealIngredientInput{IngredientID: chicken.ID, QuantityGrams: 200})
	if err != nil {
		t.Fatalf("add chicken: %v", err)
	}
	if !within(chickenLine.Macros.Calories, 330, 0.01) {
		t.Fatalf("chicken line calories = %v, want 330", chickenLine.Macros.Calories)
	}

	if _, err := planSvc.AddIngredient(ctx, meal.ID, user.ID, meals.MealIngredientInput{IngredientID: rice.ID, QuantityGrams: 150}); err != nil {
		t.Fatalf("add rice: %v", err)
	}

	// 200g chicken (330 kcal) + 150g rice (195 kcal) = 525 kcal.
	loaded, err := planSvc.GetPlan(ctx, plan.ID, user.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if len(loaded.Meals) != 1 || len(loaded.Meals[0].Ingredients) != 2 {
		t.Fatalf("expected 1 meal with 2 ingredients, got %d meals", len(loaded.Meals))
	}
	if !within(loaded.Meals[0].TotalMacros.Calories, 525, 0.01) {
		t.Fatalf("meal total calories = %v, want 525", loaded.Meals[0].TotalMacros.Calories)
	}
	if !within(loaded.TotalMacros.Calories, 525, 0.01) {
		t.Fatalf("plan total calories = %v, want 525 (only one meal)", loaded.TotalMacros.Calories)
	}

	// Removing the rice line should bring both totals back down to just the
	// chicken.
	if err := planSvc.RemoveIngredient(ctx, chickenLine.ID, user.ID); err != nil {
		t.Fatalf("remove chicken line: %v", err)
	}

	afterRemoval, err := planSvc.GetPlan(ctx, plan.ID, user.ID)
	if err != nil {
		t.Fatalf("get plan after removal: %v", err)
	}
	if len(afterRemoval.Meals[0].Ingredients) != 1 {
		t.Fatalf("expected 1 remaining ingredient, got %d", len(afterRemoval.Meals[0].Ingredients))
	}
	if !within(afterRemoval.Meals[0].TotalMacros.Calories, 195, 0.01) {
		t.Fatalf("meal total after removal = %v, want 195 (rice only)", afterRemoval.Meals[0].TotalMacros.Calories)
	}
	if !within(afterRemoval.TotalMacros.Calories, 195, 0.01) {
		t.Fatalf("plan total after removal = %v, want 195", afterRemoval.TotalMacros.Calories)
	}

	// Removing the whole meal should zero out the plan.
	if err := planSvc.RemoveMeal(ctx, meal.ID, user.ID); err != nil {
		t.Fatalf("remove meal: %v", err)
	}
	afterMealRemoval, err := planSvc.GetPlan(ctx, plan.ID, user.ID)
	if err != nil {
		t.Fatalf("get plan after meal removal: %v", err)
	}
	if len(afterMealRemoval.Meals) != 0 {
		t.Fatalf("expected no meals left, got %d", len(afterMealRemoval.Meals))
	}
	if afterMealRemoval.TotalMacros.Calories != 0 {
		t.Fatalf("plan total after removing its only meal = %v, want 0", afterMealRemoval.TotalMacros.Calories)
	}
}

func TestPlansAreScopedToTheirOwner(t *testing.T) {
	pool := testdb.New(t)
	owner := newUser(t, pool, "owner@north.test")
	stranger := newUser(t, pool, "stranger@north.test")
	repo := meals.NewRepository(pool)
	planSvc := meals.NewMealPlanService(repo)
	ctx := context.Background()

	plan, err := planSvc.CreatePlan(ctx, owner.ID, meals.MealPlanInput{Name: "My plan"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if _, err := planSvc.GetPlan(ctx, plan.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if _, err := planSvc.AddMeal(ctx, plan.ID, stranger.ID, meals.MealInput{Name: "Hijack", MealNumber: 1}); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestValidationRejectsMissingPlanName(t *testing.T) {
	t.Parallel()

	_, err := meals.ValidateMealPlan(meals.MealPlanInput{})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
