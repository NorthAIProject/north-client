package meals_test

import (
	"errors"
	"math"
	"testing"

	"github.com/NorthAIProject/north-client/internal/meals"
)

// An optional food — "compota 0% (opcional)" — is part of the meal as shown
// and part of none of its totals.
func TestAnOptionalFoodIsShownButNotCounted(t *testing.T) {
	f := newPlanFixture(t, "optional-create@north.test")
	days := []meals.DayDraft{{Meals: []meals.MealDraft{{
		Name: "Pequeno-almoço",
		Portions: []meals.MealIngredientInput{
			{IngredientID: f.rice.ID, QuantityGrams: 100},
			{IngredientID: f.chicken.ID, QuantityGrams: 200, Optional: true, SourceText: "frango (opcional)"},
		},
	}}}}
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Opcional", Settings: easyMid, DayCount: 1}, days, false)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	plan = f.reload(t, plan.ID)

	breakfast := plan.Days[0].Meals[0]
	if len(breakfast.Ingredients) != 2 {
		t.Fatalf("ingredients = %d, want both shown", len(breakfast.Ingredients))
	}
	var optional *meals.MealIngredient
	for i := range breakfast.Ingredients {
		if breakfast.Ingredients[i].Optional {
			optional = &breakfast.Ingredients[i]
		}
	}
	if optional == nil || optional.Macros.Calories == 0 || optional.SourceText != "frango (opcional)" {
		t.Fatalf("optional ingredient = %+v, want it kept with its own macros", optional)
	}
	riceOnly := f.rice.MacrosFor(100).Calories
	if got := breakfast.TotalMacros.Calories; math.Abs(got-riceOnly) > 0.01 {
		t.Errorf("meal total = %.1f kcal, want only the counted rice (%.1f)", got, riceOnly)
	}
	if got := plan.TotalMacros.Calories; math.Abs(got-riceOnly) > 0.01 {
		t.Errorf("plan total = %.1f kcal, want %.1f", got, riceOnly)
	}
}

func TestMarkingAFoodOptionalAndBackMovesTheTotals(t *testing.T) {
	f := newPlanFixture(t, "optional-toggle@north.test")
	plan := f.lunchWithOption(t)
	chicken := f.reload(t, plan.ID).Days[0].Meals[0].Ingredients[0]
	counted := f.chicken.MacrosFor(100).Calories

	if _, err := f.svc.SetIngredientOptional(f.ctx, chicken.ID, f.userID, true, false); err != nil {
		t.Fatalf("make optional: %v", err)
	}
	after := f.reload(t, plan.ID)
	if !after.Days[0].Meals[0].Ingredients[0].Optional || after.Days[0].Meals[0].TotalMacros.Calories != 0 || after.TotalMacros.Calories != 0 {
		t.Fatalf("after making it optional: meal %+v, plan total %.1f", after.Days[0].Meals[0], after.TotalMacros.Calories)
	}

	if _, err := f.svc.SetIngredientOptional(f.ctx, chicken.ID, f.userID, false, false); err != nil {
		t.Fatalf("count again: %v", err)
	}
	if got := f.reload(t, plan.ID).TotalMacros.Calories; math.Abs(got-counted) > 0.01 {
		t.Errorf("plan total after counting again = %.1f, want %.1f", got, counted)
	}
}

// Counting a food again adds it to its day, so it meets the same overage
// rule as adding it would.
func TestCountingAnOptionalFoodAgainChecksTheTarget(t *testing.T) {
	f := newPlanFixture(t, "optional-overage@north.test")
	days := []meals.DayDraft{{Meals: []meals.MealDraft{{
		Name:     "Jantar",
		Portions: []meals.MealIngredientInput{{IngredientID: f.rice.ID, QuantityGrams: 2000, Optional: true}},
	}}}}
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Muito arroz", Settings: easyMid, DayCount: 1}, days, false)
	if err != nil {
		t.Fatalf("an optional 2 kg of rice is not counted, so the plan saves: %v", err)
	}
	rice := f.reload(t, plan.ID).Days[0].Meals[0].Ingredients[0]

	_, err = f.svc.SetIngredientOptional(f.ctx, rice.ID, f.userID, false, false)
	var over *meals.OverageError
	if !errors.As(err, &over) {
		t.Fatalf("counting 2 kg of rice: err = %v, want an overage", err)
	}
	if !f.reload(t, plan.ID).Days[0].Meals[0].Ingredients[0].Optional {
		t.Fatal("a refused change was written")
	}
}

func TestPlanChangesMarkAFoodOptional(t *testing.T) {
	f := newPlanFixture(t, "optional-change@north.test")
	plan := f.lunchWithOption(t)

	_, applied, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpSetOptional, Meal: "Almoço", Food: "chicken", Optional: true},
		{Op: meals.OpAddFood, Meal: "Almoço", IngredientID: f.rice.ID, Grams: 50, Optional: true},
	}, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	after := f.reload(t, plan.ID).Days[0].Meals[0]
	if len(after.Ingredients) != 2 || !after.Ingredients[0].Optional || !after.Ingredients[1].Optional {
		t.Fatalf("ingredients = %+v, want both optional", after.Ingredients)
	}
	if after.TotalMacros.Calories != 0 {
		t.Errorf("meal total = %.1f, want 0: nothing in it is counted", after.TotalMacros.Calories)
	}
	if got := applied[0].String(); got != "made Chicken breast in Almoço optional (not counted) on Monday" {
		t.Errorf("summary = %q", got)
	}
}
