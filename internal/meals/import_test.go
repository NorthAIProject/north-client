package meals_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestImportPlanAppliesTheOverageRuleToTheWholePlan(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "import@north.test")
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	planSvc := meals.NewMealPlanService(repo)
	ctx := context.Background()

	// Low carb is 20% of 200 g: a 40 g carb day.
	goals := testMacroGoalLookup{plan: calculator.MacroPlan{ProteinG: 150, FatG: 60, CarbG: 200}}

	rice, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "White rice", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28},
	})
	if err != nil {
		t.Fatalf("create rice: %v", err)
	}

	in := meals.ImportedMealPlan{
		Name:     "Coach's week",
		PlanType: meals.PlanTypeLowCarb,
		Meals: []meals.ImportedMeal{
			{Name: "Lunch", Weekday: 1, Items: []meals.ImportedItem{{IngredientID: rice.ID, QuantityGrams: 100}}},
			// Tuesday: 200 g rice is 56 g carbs, over the 40 g target.
			{Name: "Lunch", Weekday: 2, Items: []meals.ImportedItem{{IngredientID: rice.ID, QuantityGrams: 200}}},
			{Name: "Dinner", Weekday: 2, Items: []meals.ImportedItem{{
				NewIngredient: &meals.IngredientInput{Name: "Coach's shake", Per100g: meals.Macros{Calories: 120, ProteinG: 24, FatG: 2, CarbG: 2}},
				QuantityGrams: 300,
			}}},
		},
	}

	_, err = planSvc.ImportPlan(ctx, user.ID, in, false, goals)
	var over meals.ImportOverageError
	if !apperr.As(err, &over) {
		t.Fatalf("err = %v, want an overage refusal", err)
	}
	if _, tuesday := over.Days[2]; !tuesday || len(over.Days) != 1 {
		t.Fatalf("over days = %v, want only Tuesday", over.Days)
	}

	plans, err := planSvc.ListPlans(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("a refused import stored %d plans", len(plans))
	}
	found, err := ingredientSvc.Search(ctx, user.ID, "Coach's shake", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("a refused import created its personal ingredient")
	}

	plan, err := planSvc.ImportPlan(ctx, user.ID, in, true, goals)
	if err != nil {
		t.Fatalf("confirmed import: %v", err)
	}
	if plan.PlanType != meals.PlanTypeLowCarb || len(plan.Meals) != 3 {
		t.Fatalf("plan = %+v", plan)
	}

	var sum meals.Macros
	for _, m := range plan.Meals {
		sum = sum.Add(m.TotalMacros)
		wantConfirmed := *m.Weekday == 2
		if m.OverageConfirmed != wantConfirmed {
			t.Errorf("%s on %d: overage confirmed = %v, want %v", m.Name, *m.Weekday, m.OverageConfirmed, wantConfirmed)
		}
		if *m.Weekday == 2 && m.Name == "Dinner" {
			if m.MealNumber != 2 {
				t.Errorf("Tuesday dinner meal number = %d, want 2", m.MealNumber)
			}
			if !within(m.TotalMacros.ProteinG, 72, 0.01) {
				t.Errorf("shake protein = %v, want 72 from the stated per-100g", m.TotalMacros.ProteinG)
			}
		}
	}
	if !within(plan.TotalMacros.CarbG, sum.CarbG, 0.01) || !within(sum.CarbG, 28+56+6, 0.01) {
		t.Fatalf("plan carbs = %v, meals sum %v", plan.TotalMacros.CarbG, sum.CarbG)
	}
}

func TestImportPlanWithoutAPlanTypeHasNoTargetToExceed(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "import-untyped@north.test")
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	planSvc := meals.NewMealPlanService(repo)
	ctx := context.Background()

	rice, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "White rice", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28},
	})
	if err != nil {
		t.Fatal(err)
	}

	goals := testMacroGoalLookup{plan: calculator.MacroPlan{ProteinG: 150, FatG: 60, CarbG: 20}}
	_, err = planSvc.ImportPlan(ctx, user.ID, meals.ImportedMealPlan{
		Name:  "Untyped",
		Meals: []meals.ImportedMeal{{Name: "Lunch", Weekday: 1, Items: []meals.ImportedItem{{IngredientID: rice.ID, QuantityGrams: 500}}}},
	}, false, goals)
	if err != nil {
		t.Fatalf("import: %v (a plan with no carb type is not measured, as when built by hand)", err)
	}
}

func TestImportPlanRefusesUnresolvedFoods(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "import-bad@north.test")
	planSvc := meals.NewMealPlanService(meals.NewRepository(pool))
	ctx := context.Background()

	cases := map[string]meals.ImportedMealPlan{
		"no name":       {Name: " ", Meals: []meals.ImportedMeal{{Name: "Lunch", Weekday: 1, Items: []meals.ImportedItem{{QuantityGrams: 1}}}}},
		"no ingredient": {Name: "x", Meals: []meals.ImportedMeal{{Name: "Lunch", Weekday: 1, Items: []meals.ImportedItem{{QuantityGrams: 100}}}}},
		"no grams":      {Name: "x", Meals: []meals.ImportedMeal{{Name: "Lunch", Weekday: 1, Items: []meals.ImportedItem{{NewIngredient: &meals.IngredientInput{Name: "y"}}}}}},
		"bad weekday":   {Name: "x", Meals: []meals.ImportedMeal{{Name: "Lunch", Weekday: 9, Items: []meals.ImportedItem{{NewIngredient: &meals.IngredientInput{Name: "y"}, QuantityGrams: 1}}}}},
		"no meals":      {Name: "x"},
	}
	for name, in := range cases {
		if _, err := planSvc.ImportPlan(ctx, user.ID, in, true, nil); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: err = %v, want validation", name, err)
		}
	}
}
