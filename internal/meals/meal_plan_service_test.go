package meals_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/calculator"
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

	if _, err = planSvc.AddIngredient(ctx, meal.ID, user.ID, meals.MealIngredientInput{IngredientID: rice.ID, QuantityGrams: 150}); err != nil {
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
	if err = planSvc.RemoveIngredient(ctx, chickenLine.ID, user.ID); err != nil {
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
	if err = planSvc.RemoveMeal(ctx, meal.ID, user.ID); err != nil {
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

// A spoken meal lands as one batch. Every line reaches the meal and the totals
// still equal the sum of their children — the cache the test above guards.
func TestAddIngredientsAddsEveryLine(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "batch@north.test")
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	planSvc := meals.NewMealPlanService(repo)
	ctx := context.Background()

	chicken, rice, meal := batchFixture(t, ctx, ingredientSvc, planSvc, user.ID)

	added, err := planSvc.AddIngredients(ctx, meal.ID, user.ID, []meals.MealIngredientInput{
		{IngredientID: chicken.ID, QuantityGrams: 200},
		{IngredientID: rice.ID, QuantityGrams: 150},
	})
	if err != nil {
		t.Fatalf("add ingredients: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("added %d lines, want 2", len(added))
	}

	// 200g chicken (330 kcal) + 150g rice (195 kcal) = 525 kcal.
	loaded, err := planSvc.GetPlan(ctx, meal.MealPlanID, user.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if !within(loaded.Meals[0].TotalMacros.Calories, 525, 0.01) {
		t.Fatalf("meal total calories = %v, want 525", loaded.Meals[0].TotalMacros.Calories)
	}
}

// One bad line refuses the batch before anything is written: half a spoken
// meal on the plan, with no way to tell which half, is worse than none.
func TestAddIngredientsWritesNothingWhenOneLineIsBad(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "badbatch@north.test")
	stranger := newUser(t, pool, "batchstranger@north.test")
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	planSvc := meals.NewMealPlanService(repo)
	ctx := context.Background()

	chicken, _, meal := batchFixture(t, ctx, ingredientSvc, planSvc, user.ID)
	private, err := ingredientSvc.Create(ctx, stranger.ID, meals.IngredientInput{
		Name: "Stranger's granola", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 450},
	})
	if err != nil {
		t.Fatalf("create stranger's ingredient: %v", err)
	}

	for name, lines := range map[string][]meals.MealIngredientInput{
		"zero grams":                {{IngredientID: chicken.ID, QuantityGrams: 200}, {IngredientID: chicken.ID, QuantityGrams: 0}},
		"another account's private": {{IngredientID: chicken.ID, QuantityGrams: 200}, {IngredientID: private.ID, QuantityGrams: 50}},
		"empty":                     nil,
	} {
		if _, refused := planSvc.AddIngredients(ctx, meal.ID, user.ID, lines); refused == nil {
			t.Errorf("%s: batch was accepted", name)
		}
	}

	loaded, err := planSvc.GetPlan(ctx, meal.MealPlanID, user.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if n := len(loaded.Meals[0].Ingredients); n != 0 {
		t.Fatalf("a refused batch left %d lines on the meal", n)
	}
}

func batchFixture(t *testing.T, ctx context.Context, ingredientSvc *meals.IngredientService, planSvc *meals.MealPlanService, userID uuid.UUID) (meals.Ingredient, meals.Ingredient, meals.Meal) {
	t.Helper()

	chicken, err := ingredientSvc.Create(ctx, userID, meals.IngredientInput{
		Name: "Chicken breast", Category: meals.CategoryProtein,
		Per100g: meals.Macros{Calories: 165, ProteinG: 31, FatG: 3.6, CarbG: 0},
	})
	if err != nil {
		t.Fatalf("create chicken: %v", err)
	}
	rice, err := ingredientSvc.Create(ctx, userID, meals.IngredientInput{
		Name: "White rice", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28},
	})
	if err != nil {
		t.Fatalf("create rice: %v", err)
	}
	plan, err := planSvc.CreatePlan(ctx, userID, meals.MealPlanInput{Name: "Spoken plan"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	meal, err := planSvc.AddMeal(ctx, plan.ID, userID, meals.MealInput{Name: "Lunch", MealNumber: 1})
	if err != nil {
		t.Fatalf("add meal: %v", err)
	}
	return chicken, rice, meal
}

type testMacroGoalLookup struct {
	plan calculator.MacroPlan
}

func (m testMacroGoalLookup) Current(ctx context.Context, userID uuid.UUID) (calculator.MacroPlan, error) {
	return m.plan, nil
}

func TestFlexibleCarbPlanAndOverageEnforcement(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "carbplans@north.test")
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	planSvc := meals.NewMealPlanService(repo)
	ctx := context.Background()

	goals := testMacroGoalLookup{
		plan: calculator.MacroPlan{
			ProteinG:    150,
			FatG:        60,
			CarbG:       200,
			CalorieGoal: 150*4 + 60*9 + 200*4,
		},
	}

	rice, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name:     "White rice",
		Category: meals.CategoryCarb,
		Per100g:  meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28},
	})
	if err != nil {
		t.Fatalf("create rice: %v", err)
	}

	// 1. Create plan with low_carb
	plan, err := planSvc.CreatePlan(ctx, user.ID, meals.MealPlanInput{
		Name:     "Flexible Carb Week",
		PlanType: meals.PlanTypeLowCarb,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if plan.PlanType != meals.PlanTypeLowCarb {
		t.Fatalf("expected plan type low_carb, got %v", plan.PlanType)
	}

	// 2. Add Monday meal (weekday 0) and Saturday meal (weekday 5)
	weekdayMon := 0
	monMeal, err := planSvc.AddMeal(ctx, plan.ID, user.ID, meals.MealInput{
		Name:        "Monday Lunch",
		MealNumber:  1,
		Weekday:     &weekdayMon,
		DayPlanType: meals.PlanTypeLowCarb,
	})
	if err != nil {
		t.Fatalf("add monday meal: %v", err)
	}

	weekdaySat := 5
	satMeal, err := planSvc.AddMeal(ctx, plan.ID, user.ID, meals.MealInput{
		Name:        "Saturday Feast",
		MealNumber:  1,
		Weekday:     &weekdaySat,
		DayPlanType: meals.PlanTypeHighCarb,
	})
	if err != nil {
		t.Fatalf("add saturday meal: %v", err)
	}

	// Resolve targets: Monday is low_carb (20% of 200 = 40g carb)
	monTarget := meals.ResolveDayTarget(goals.plan.ProteinG, goals.plan.FatG, goals.plan.CarbG, plan.PlanType, plan.CustomCarbPct, monMeal.DayPlanType, nil, nil, nil)
	if !within(monTarget.CarbG, 40.0, 0.01) {
		t.Fatalf("monday carb target = %v, want 40", monTarget.CarbG)
	}

	// Saturday is high_carb (55% of 200 = 110g carb)
	satTarget := meals.ResolveDayTarget(goals.plan.ProteinG, goals.plan.FatG, goals.plan.CarbG, plan.PlanType, plan.CustomCarbPct, satMeal.DayPlanType, nil, nil, nil)
	if !within(satTarget.CarbG, 110.0, 0.01) {
		t.Fatalf("saturday carb target = %v, want 110", satTarget.CarbG)
	}

	// 3. Adding 200g rice to Monday: 200g * 28g/100g = 56g carbs.
	// Since Monday limit is 40g, 56g exceeds the 40g target by 16g.
	// Without confirmation, AddIngredientChecked must be refused and rollback cleanly.
	_, overage, err := planSvc.AddIngredientChecked(ctx, monMeal.ID, user.ID, meals.MealIngredientInput{
		IngredientID:  rice.ID,
		QuantityGrams: 200,
	}, false, goals)
	if err == nil {
		t.Fatalf("expected overage error, got nil")
	}

	var overageErr meals.OverageError
	if !apperr.As(err, &overageErr) {
		t.Fatalf("expected error to be meals.OverageError, got %T: %v", err, err)
	}
	if overage == nil || !overage.IsOver {
		t.Fatalf("expected overage.IsOver to be true")
	}
	if !within(overage.CarbG, 16.0, 0.01) {
		t.Fatalf("carb overage = %v, want 16", overage.CarbG)
	}

	// Verify nothing was saved
	loadedMon, err := repo.GetMeal(ctx, monMeal.ID, user.ID)
	if err != nil {
		t.Fatalf("get meal: %v", err)
	}
	if len(loadedMon.Ingredients) != 0 {
		t.Fatalf("expected 0 ingredients after rejected overage, got %d", len(loadedMon.Ingredients))
	}

	// 4. Try again with confirmOverage = true
	savedLine, overage, err := planSvc.AddIngredientChecked(ctx, monMeal.ID, user.ID, meals.MealIngredientInput{
		IngredientID:  rice.ID,
		QuantityGrams: 200,
	}, true, goals)
	if err != nil {
		t.Fatalf("add ingredient with confirmation failed: %v", err)
	}
	if savedLine.QuantityGrams != 200 {
		t.Fatalf("expected 200g saved line, got %v", savedLine.QuantityGrams)
	}
	if overage == nil || !overage.IsOver {
		t.Fatalf("expected overage to still be calculated and returned even when confirmed")
	}

	// Verify meal now has the line and overage_confirmed = true
	loadedMon, err = repo.GetMeal(ctx, monMeal.ID, user.ID)
	if err != nil {
		t.Fatalf("get meal: %v", err)
	}
	if len(loadedMon.Ingredients) != 1 {
		t.Fatalf("expected 1 ingredient, got %d", len(loadedMon.Ingredients))
	}
	if !loadedMon.OverageConfirmed {
		t.Fatalf("expected overage_confirmed to be true on meal")
	}

	// 5. On Saturday, limit is 110g carb. Adding 200g rice (56g carb) is within 110g.
	// Should succeed without overage confirmation.
	satLine, satOverage, err := planSvc.AddIngredientChecked(ctx, satMeal.ID, user.ID, meals.MealIngredientInput{
		IngredientID:  rice.ID,
		QuantityGrams: 200,
	}, false, goals)
	if err != nil {
		t.Fatalf("add ingredient to saturday failed: %v", err)
	}
	if satLine.QuantityGrams != 200 {
		t.Fatalf("expected 200g, got %v", satLine.QuantityGrams)
	}
	if satOverage != nil && satOverage.IsOver {
		t.Fatalf("expected saturday overage to not be over")
	}
}

