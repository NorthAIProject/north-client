package meals_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// easyPlan is an easy plan of n days from Monday.
func (f planFixture) easyPlan(t *testing.T, s meal.PlanSettings, n int) meals.MealPlan {
	t.Helper()
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Plan", Settings: s, DayCount: n}, nil, false)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan
}

// sumOfMeals is what a plan's cached total should be.
func sumOfMeals(plan meals.MealPlan) meals.Macros {
	var total meals.Macros
	for _, d := range plan.Days {
		total = total.Add(d.Consumed())
	}
	return total
}

func TestApplyChangesTargetsEveryDayWhenNoneAreNamed(t *testing.T) {
	f := newPlanFixture(t, "changes-everyday@north.test")
	plan := f.easyPlan(t, easyMid, 3)

	stored, applied, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpAddMeal, Meal: "Breakfast"},
		{Op: meals.OpAddFood, Meal: "breakfast", IngredientID: f.chicken.ID, Grams: 100},
	}, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(applied) != 2 || len(applied[1].Weekdays) != 3 || applied[1].Food != "Chicken breast" {
		t.Fatalf("applied = %+v", applied)
	}
	for _, d := range stored.Days {
		if len(d.Meals) != 1 || d.Meals[0].Name != "Breakfast" || len(d.Meals[0].Ingredients) != 1 {
			t.Fatalf("%s = %+v", d.Weekday, d.Meals)
		}
		if got := d.Meals[0].TotalMacros.ProteinG; !within(got, 31, 0.001) {
			t.Fatalf("%s breakfast protein = %v, want 31", d.Weekday, got)
		}
	}
	if got := stored.TotalMacros.ProteinG; !within(got, 93, 0.001) {
		t.Fatalf("plan protein = %v, want 93", got)
	}
}

func TestApplyChangesEditsOnlyTheNamedDays(t *testing.T) {
	f := newPlanFixture(t, "changes-named@north.test")
	plan := f.easyPlan(t, easyMid, 2)
	for _, d := range plan.Days {
		lunch := f.meal(t, d, "Lunch")
		if err := f.add(lunch, f.chicken, 100, false); err != nil {
			t.Fatalf("add chicken: %v", err)
		}
		// A second portion of the same food: set_grams leaves one.
		if err := f.add(lunch, f.chicken, 20, false); err != nil {
			t.Fatalf("add chicken: %v", err)
		}
		f.meal(t, d, "Snack")
	}

	stored, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpSetGrams, Days: []time.Weekday{time.Tuesday}, Meal: "lunch", Food: "chicken", Grams: 50},
		{Op: meals.OpRemoveMeal, Days: []time.Weekday{time.Tuesday}, Meal: "snack"},
	}, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	monday, tuesday := stored.Days[0], stored.Days[1]
	if len(monday.Meals) != 2 || len(monday.Meals[0].Ingredients) != 2 {
		t.Fatalf("Monday changed: %+v", monday.Meals)
	}
	if len(tuesday.Meals) != 1 {
		t.Fatalf("Tuesday's snack is still there: %+v", tuesday.Meals)
	}
	lunch := tuesday.Meals[0]
	if len(lunch.Ingredients) != 1 || lunch.Ingredients[0].QuantityGrams != 50 {
		t.Fatalf("Tuesday lunch = %+v", lunch.Ingredients)
	}
	// The snapshot is scaled: 50 g of 31 g protein per 100 g.
	if got := lunch.TotalMacros.ProteinG; !within(got, 15.5, 0.001) {
		t.Fatalf("Tuesday lunch protein = %v, want 15.5", got)
	}
	if got, want := stored.TotalMacros.ProteinG, sumOfMeals(stored).ProteinG; !within(got, want, 0.001) {
		t.Fatalf("plan total %v drifted from its meals' %v", got, want)
	}
}

func TestApplyChangesWritesNothingWhenOneChangeMatchesNothing(t *testing.T) {
	f := newPlanFixture(t, "changes-atomic@north.test")
	plan := f.easyPlan(t, easyMid, 2)
	lunch := f.meal(t, plan.Days[0], "Lunch")
	if err := f.add(lunch, f.chicken, 100, false); err != nil {
		t.Fatalf("add chicken: %v", err)
	}
	before := f.reload(t, plan.ID)

	for name, changes := range map[string][]meals.PlanChange{
		"food not in the meal": {
			{Op: meals.OpAddFood, Meal: "Lunch", IngredientID: f.rice.ID, Grams: 50},
			{Op: meals.OpRemoveFood, Meal: "Lunch", Food: "salmon"},
		},
		"meal on no day": {
			{Op: meals.OpRemoveMeal, Meal: "Lunch"},
			{Op: meals.OpSetGrams, Meal: "Dinner", Food: "chicken", Grams: 10},
		},
		"weekday not in the plan": {
			{Op: meals.OpAddMeal, Meal: "Dinner"},
			{Op: meals.OpRemoveMeal, Days: []time.Weekday{time.Sunday}, Meal: "Lunch"},
		},
		"unknown op": {
			{Op: meals.OpAddMeal, Meal: "Dinner"},
			{Op: "rename_meal", Meal: "Lunch"},
		},
	} {
		_, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, changes, false)
		if !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}

	after := f.reload(t, plan.ID)
	if len(after.Days[0].Meals) != 1 || len(after.Days[0].Meals[0].Ingredients) != 1 || len(after.Days[1].Meals) != 0 {
		t.Fatalf("a refused batch was partly written: %+v", after.Days)
	}
	if after.TotalMacros != before.TotalMacros {
		t.Fatalf("totals moved: %+v -> %+v", before.TotalMacros, after.TotalMacros)
	}
}

func TestApplyChangesOverageLeavesThePlanUnchanged(t *testing.T) {
	f := newPlanFixture(t, "changes-easyover@north.test")
	plan := f.plan(t, easyLow)

	// 200 g rice is 56 g carbs against a 38.75 g day.
	changes := []meals.PlanChange{
		{Op: meals.OpAddMeal, Meal: "Lunch"},
		{Op: meals.OpAddFood, Meal: "Lunch", IngredientID: f.rice.ID, Grams: 200},
	}
	for _, confirm := range []bool{false, true} {
		if over := overage(t, applyErr(f, plan, changes, confirm)); over.CanConfirm {
			t.Fatal("an easy plan offered to confirm an overage")
		}
	}
	if loaded := f.reload(t, plan.ID); len(loaded.Days[0].Meals) != 0 || loaded.TotalMacros.Calories != 0 {
		t.Fatalf("a refused batch was written: %+v", loaded.Days[0])
	}
}

func TestApplyChangesConfirmAllowsAnAdvancedOverage(t *testing.T) {
	f := newPlanFixture(t, "changes-advancedover@north.test")
	plan := f.plan(t, meal.PlanSettings{Type: meal.LowCarb, Mode: meal.Advanced}, time.Monday, time.Thursday)
	changes := []meals.PlanChange{
		{Op: meals.OpAddMeal, Days: []time.Weekday{time.Thursday}, Meal: "Lunch"},
		{Op: meals.OpAddFood, Days: []time.Weekday{time.Thursday}, Meal: "Lunch", IngredientID: f.rice.ID, Grams: 200},
	}

	over := overage(t, applyErr(f, plan, changes, false))
	if !over.CanConfirm || len(over.Over) != 1 || over.Over[0].Weekday != time.Thursday {
		t.Fatalf("verdict = %+v", over)
	}
	if loaded := f.reload(t, plan.ID); len(loaded.Days[1].Meals) != 0 {
		t.Fatal("a refused batch was written")
	}

	stored, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, changes, true)
	if err != nil {
		t.Fatalf("confirmed overage refused: %v", err)
	}
	if got := stored.Days[1].Consumed().CarbG; !within(got, 56, 0.001) {
		t.Fatalf("Thursday carbs = %v, want 56", got)
	}
}

func applyErr(f planFixture, plan meals.MealPlan, changes []meals.PlanChange, confirm bool) error {
	_, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, changes, confirm)
	return err
}
