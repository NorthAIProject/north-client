package meals_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"testing"

	"github.com/NorthAIProject/north-client/internal/meals"
)

// An optional meal — "Ceia (opcional)" — is part of the day as shown and part
// of none of its totals; its own total is still worked out.
func TestAnOptionalMealIsShownButNotCounted(t *testing.T) {
	f := newPlanFixture(t, "optional-meal-create@north.test")
	days := []meals.DayDraft{{Meals: []meals.MealDraft{
		{Name: "Almoço", Portions: []meals.MealIngredientInput{{IngredientID: f.rice.ID, QuantityGrams: 100}}},
		{Name: "Ceia", Optional: true, Portions: []meals.MealIngredientInput{{IngredientID: f.chicken.ID, QuantityGrams: 200}}},
	}}}
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Ceia opcional", Settings: easyMid, DayCount: 1}, days, false)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	plan = f.reload(t, plan.ID)

	day := plan.Days[0]
	if len(day.Meals) != 2 || !day.Meals[1].Optional || day.Meals[0].Optional {
		t.Fatalf("meals = %+v, want Almoço counted and Ceia optional", day.Meals)
	}
	if got, want := day.Meals[1].TotalMacros.Calories, f.chicken.MacrosFor(200).Calories; math.Abs(got-want) > 0.01 {
		t.Errorf("Ceia's own total = %.1f kcal, want %.1f", got, want)
	}
	riceOnly := f.rice.MacrosFor(100).Calories
	if got := day.Consumed().Calories; math.Abs(got-riceOnly) > 0.01 {
		t.Errorf("day consumed = %.1f kcal, want only Almoço (%.1f)", got, riceOnly)
	}
	if got := plan.TotalMacros.Calories; math.Abs(got-riceOnly) > 0.01 {
		t.Errorf("plan total = %.1f kcal, want %.1f", got, riceOnly)
	}
}

// The flag belongs to the slot: every option carries it, an option added
// later takes it, and counting the meal again counts its first option.
func TestMarkingAMealOptionalAndBackMovesTheTotals(t *testing.T) {
	f := newPlanFixture(t, "optional-meal-toggle@north.test")
	plan := f.lunchWithOption(t)
	slot := f.reload(t, plan.ID).Days[0].Meals[0]
	counted := f.chicken.MacrosFor(100).Calories

	// Named by its second option, the change still covers the whole slot.
	updated, err := f.svc.SetMealOptional(f.ctx, slot.Alternatives[0].ID, f.userID, true, false)
	if err != nil {
		t.Fatalf("make optional: %v", err)
	}
	after := updated.Days[0].Meals[0]
	if !after.Optional || !after.Alternatives[0].Optional {
		t.Fatalf("slot = %+v, want every option optional", after)
	}
	if updated.TotalMacros.Calories != 0 || updated.Days[0].Consumed().Calories != 0 {
		t.Fatalf("plan total %.1f, day %.1f, want nothing counted", updated.TotalMacros.Calories, updated.Days[0].Consumed().Calories)
	}
	if math.Abs(after.TotalMacros.Calories-counted) > 0.01 {
		t.Errorf("the meal's own total = %.1f, want it kept (%.1f)", after.TotalMacros.Calories, counted)
	}

	added, err := f.svc.AddOption(f.ctx, f.userID, plan.ID, slot.ID, "Opção 3")
	if err != nil {
		t.Fatalf("add option: %v", err)
	}
	if !added.Optional {
		t.Error("an option added to an optional meal is counted, want it optional like its slot")
	}

	if _, err := f.svc.SetMealOptional(f.ctx, slot.ID, f.userID, false, false); err != nil {
		t.Fatalf("count again: %v", err)
	}
	again := f.reload(t, plan.ID)
	for _, o := range again.Days[0].Meals[0].Options() {
		if o.Optional {
			t.Errorf("option %q still optional after counting the meal again", o.DisplayName())
		}
	}
	if got := again.TotalMacros.Calories; math.Abs(got-counted) > 0.01 {
		t.Errorf("plan total after counting again = %.1f, want %.1f", got, counted)
	}
}

// Counting a meal again adds it to its day, so it meets the same overage rule
// as adding its food would; adding food to an optional meal never does.
func TestCountingAnOptionalMealAgainChecksTheTarget(t *testing.T) {
	f := newPlanFixture(t, "optional-meal-overage@north.test")
	days := []meals.DayDraft{{Meals: []meals.MealDraft{{
		Name: "Pré-treino", Optional: true,
		Portions: []meals.MealIngredientInput{{IngredientID: f.rice.ID, QuantityGrams: 2000}},
	}}}}
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Muito arroz", Settings: easyMid, DayCount: 1}, days, false)
	if err != nil {
		t.Fatalf("an optional meal of 2 kg of rice is not counted, so the plan saves: %v", err)
	}
	slot := f.reload(t, plan.ID).Days[0].Meals[0]

	if _, err = f.svc.AddIngredients(f.ctx, slot.ID, f.userID, []meals.MealIngredientInput{{IngredientID: f.rice.ID, QuantityGrams: 1000}}, false); err != nil {
		t.Fatalf("adding to an optional meal is never an overage: %v", err)
	}

	_, err = f.svc.SetMealOptional(f.ctx, slot.ID, f.userID, false, false)
	var over *meals.OverageError
	if !errors.As(err, &over) {
		t.Fatalf("counting 3 kg of rice: err = %v, want an overage", err)
	}
	if !f.reload(t, plan.ID).Days[0].Meals[0].Optional {
		t.Fatal("a refused change was written")
	}
}

func TestPlanChangesMarkAMealOptional(t *testing.T) {
	f := newPlanFixture(t, "optional-meal-change@north.test")
	plan := f.lunchWithOption(t)

	_, applied, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpSetMealOptional, Meal: "Almoço", Optional: true},
		{Op: meals.OpAddMeal, Meal: "Ceia", Optional: true},
		{Op: meals.OpAddFood, Meal: "Ceia", IngredientID: f.rice.ID, Grams: 50},
	}, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	after := f.reload(t, plan.ID)
	day := after.Days[0]
	if len(day.Meals) != 2 || !day.Meals[0].Optional || !day.Meals[0].Alternatives[0].Optional || !day.Meals[1].Optional {
		t.Fatalf("meals = %+v, want Almoço (every option) and Ceia optional", day.Meals)
	}
	if after.TotalMacros.Calories != 0 {
		t.Errorf("plan total = %.1f, want 0: no meal is counted", after.TotalMacros.Calories)
	}
	if got := applied[0].String(); got != "made Almoço optional (not counted) on Monday" {
		t.Errorf("summary = %q", got)
	}
	if got := applied[1].String(); got != "added the meal Ceia as optional on Monday" {
		t.Errorf("summary = %q", got)
	}

	if _, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpSetMealOptional, Meal: "Almoço", Option: 2, Optional: false},
	}, false); err == nil {
		t.Error("set_meal_optional on one option was accepted, want it refused: the flag is the whole meal's")
	}
}

func TestAPIMarksAMealOptional(t *testing.T) {
	f := newPlanFixture(t, "optional-meal-api@north.test")
	plan := f.lunchWithOption(t)
	slot := f.reload(t, plan.ID).Days[0].Meals[0]
	target := "/nutrition/meals/" + slot.ID.String() + "/optional"

	rec := f.putJSON(t, f.userID, target, `{"optional":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var detail meals.PlanDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	got := detail.Days[0]
	if !got.Meals[0].Optional || got.Meals[0].TotalMacros.Calories == 0 {
		t.Fatalf("meal = %+v, want it optional with its own total", got.Meals[0])
	}
	if got.Status != nil && got.Status.Consumed.Calories != 0 {
		t.Errorf("day consumed = %.1f, want 0", got.Status.Consumed.Calories)
	}

	stranger := newUser(t, f.pool, "optional-meal-api-stranger@north.test")
	if rec = f.putJSON(t, stranger.ID, target, `{"optional":false}`); rec.Code != http.StatusNotFound {
		t.Fatalf("another person's meal: status = %d", rec.Code)
	}
	if !f.reload(t, plan.ID).Days[0].Meals[0].Optional {
		t.Fatal("a stranger changed the meal")
	}
}
