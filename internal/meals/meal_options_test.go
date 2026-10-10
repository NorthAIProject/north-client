package meals_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// lunchWithOption is a one-day plan whose lunch is 100 g chicken (31 g
// protein), with a second option of 1000 g chicken (310 g protein) — past
// the fixture's 150 g target on its own.
func (f planFixture) lunchWithOption(t *testing.T) meals.MealPlan {
	t.Helper()
	days := []meals.DayDraft{{Meals: []meals.MealDraft{{
		Name:     "Almoço",
		Portions: []meals.MealIngredientInput{{IngredientID: f.chicken.ID, QuantityGrams: 100}},
		Alternatives: []meals.MealOptionDraft{{
			Label: "Opção 2",
			Portions: []meals.MealIngredientInput{{
				IngredientID: f.chicken.ID, QuantityGrams: 1000,
				SourceText: "frango à vontade", Estimated: true,
			}},
		}},
	}}}}
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{
		Name: "Options", Settings: easyMid, DayCount: 1, Notes: "Beber 2 L de água.",
	}, days, false)
	if err != nil {
		t.Fatalf("create plan with an option: %v", err)
	}
	return plan
}

func TestCreatePlanNestsOptionsAndCountsOnlyTheDefault(t *testing.T) {
	f := newPlanFixture(t, "options-create@north.test")
	plan := f.reload(t, f.lunchWithOption(t).ID)

	if plan.Notes != "Beber 2 L de água." {
		t.Fatalf("notes = %q", plan.Notes)
	}
	day := plan.Days[0]
	if len(day.Meals) != 1 {
		t.Fatalf("day meals = %+v, want the default only", day.Meals)
	}
	lunch := day.Meals[0]
	if lunch.OptionIndex != 1 || len(lunch.Alternatives) != 1 {
		t.Fatalf("lunch = option %d with %d alternatives", lunch.OptionIndex, len(lunch.Alternatives))
	}
	alt := lunch.Alternatives[0]
	if alt.OptionIndex != 2 || alt.OptionLabel != "Opção 2" || alt.Name != "Almoço" || alt.MealNumber != lunch.MealNumber {
		t.Fatalf("alternative = %+v", alt)
	}
	if got := alt.TotalMacros.ProteinG; !within(got, 310, 0.001) {
		t.Fatalf("alternative's own protein = %v, want 310", got)
	}
	if len(alt.Ingredients) != 1 || alt.Ingredients[0].SourceText != "frango à vontade" || !alt.Ingredients[0].Estimated {
		t.Fatalf("alternative's portion = %+v", alt.Ingredients)
	}
	if got := day.Consumed().ProteinG; !within(got, 31, 0.001) {
		t.Fatalf("day consumed protein = %v, want 31", got)
	}
	if got := plan.TotalMacros.ProteinG; !within(got, 31, 0.001) {
		t.Fatalf("plan protein = %v, want 31 (the default only)", got)
	}
}

func TestAddingToAnAlternativeIsNeverAnOverage(t *testing.T) {
	f := newPlanFixture(t, "options-add@north.test")
	plan := f.lunchWithOption(t)
	alt := plan.Days[0].Meals[0].Alternatives[0]

	if err := f.add(alt, f.chicken, 1000, false); err != nil {
		t.Fatalf("add to an alternative: %v", err)
	}
	// The same food on the default is still refused.
	overage(t, f.add(plan.Days[0].Meals[0], f.chicken, 1000, false))

	stored := f.reload(t, plan.ID)
	if got := stored.Days[0].Meals[0].Alternatives[0].TotalMacros.ProteinG; !within(got, 620, 0.001) {
		t.Fatalf("alternative protein = %v, want 620", got)
	}
	if got := stored.TotalMacros.ProteinG; !within(got, 31, 0.001) {
		t.Fatalf("plan protein = %v, want 31", got)
	}
}

func TestAddOptionAppendsAnEmptyAlternative(t *testing.T) {
	f := newPlanFixture(t, "options-addoption@north.test")
	plan := f.lunchWithOption(t)
	slot := plan.Days[0].Meals[0]

	// Any option of the slot names it.
	added, err := f.svc.AddOption(f.ctx, f.userID, plan.ID, slot.Alternatives[0].ID, " Opção 3 ")
	if err != nil {
		t.Fatalf("add option: %v", err)
	}
	if added.OptionIndex != 3 || added.OptionLabel != "Opção 3" || added.Name != "Almoço" || added.MealNumber != slot.MealNumber {
		t.Fatalf("added = %+v", added)
	}
	// A blank label is the first "Option N" the slot does not use yet.
	unlabelled, err := f.svc.AddOption(f.ctx, f.userID, plan.ID, slot.ID, "  ")
	if err != nil || unlabelled.OptionLabel != "Option 2" {
		t.Fatalf("unlabelled option = %+v, err = %v; want Option 2", unlabelled, err)
	}
	other := f.easyPlan(t, easyMid, 1)
	if _, err := f.svc.AddOption(f.ctx, f.userID, other.ID, slot.ID, "Opção 4"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("meal of another plan err = %v", err)
	}
	if got := f.reload(t, plan.ID).Days[0].Meals[0].Alternatives; len(got) != 3 || got[1].ID != added.ID {
		t.Fatalf("alternatives = %+v", got)
	}
}

// Removing a middle option frees its "Option N"; a blank add takes the first
// free number rather than the slot's size, which would repeat "Option 3" and
// have a logged "Option 3" land on either.
func TestAddOptionTakesTheFirstFreeOptionNumber(t *testing.T) {
	f := newPlanFixture(t, "options-free-number@north.test")
	days := []meals.DayDraft{{Meals: []meals.MealDraft{{
		Name:     "Almoço",
		Portions: []meals.MealIngredientInput{{IngredientID: f.chicken.ID, QuantityGrams: 100}},
		Alternatives: []meals.MealOptionDraft{
			{Label: "Option 2", Portions: []meals.MealIngredientInput{{IngredientID: f.rice.ID, QuantityGrams: 100}}},
			{Label: "OPTION 3", Portions: []meals.MealIngredientInput{{IngredientID: f.rice.ID, QuantityGrams: 150}}},
		},
	}}}}
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Free numbers", Settings: easyMid, DayCount: 1}, days, false)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	slot := plan.Days[0].Meals[0]
	if err = f.svc.RemoveMeal(f.ctx, slot.Alternatives[0].ID, f.userID); err != nil {
		t.Fatalf("remove option 2: %v", err)
	}

	added, err := f.svc.AddOption(f.ctx, f.userID, plan.ID, slot.ID, "")
	if err != nil || added.OptionLabel != "Option 2" {
		t.Fatalf("blank add = %+v, err = %v; want Option 2", added, err)
	}
	// "OPTION 3" is taken whatever its case, so the next is 4.
	next, err := f.svc.AddOption(f.ctx, f.userID, plan.ID, slot.ID, "")
	if err != nil || next.OptionLabel != "Option 4" {
		t.Fatalf("second blank add = %+v, err = %v; want Option 4", next, err)
	}
}

func TestAddOptionRefusesALabelOverTheCap(t *testing.T) {
	f := newPlanFixture(t, "options-label-cap@north.test")
	plan := f.lunchWithOption(t)
	slot := plan.Days[0].Meals[0]

	_, err := f.svc.AddOption(f.ctx, f.userID, plan.ID, slot.ID, strings.Repeat("ç", meals.MaxOptionLabelRunes+1))
	var fields apperr.FieldErrors
	if !errors.As(err, &fields) || len(fields) != 1 || fields[0].Field != "option_label" {
		t.Fatalf("err = %v, want an option_label field error", err)
	}
	if _, err := f.svc.AddOption(f.ctx, f.userID, plan.ID, slot.ID, strings.Repeat("ç", meals.MaxOptionLabelRunes)); err != nil {
		t.Fatalf("a label at the cap: %v", err)
	}
}

func TestRemoveMealOnAnAlternativeRemovesOnlyIt(t *testing.T) {
	f := newPlanFixture(t, "options-remove-alt@north.test")
	plan := f.lunchWithOption(t)
	slot := plan.Days[0].Meals[0]

	if err := f.svc.RemoveMeal(f.ctx, slot.Alternatives[0].ID, f.userID); err != nil {
		t.Fatalf("remove alternative: %v", err)
	}
	stored := f.reload(t, plan.ID)
	if len(stored.Days[0].Meals) != 1 || len(stored.Days[0].Meals[0].Alternatives) != 0 {
		t.Fatalf("meals = %+v", stored.Days[0].Meals)
	}
}

func TestRemoveMealOnTheDefaultRemovesTheSlot(t *testing.T) {
	f := newPlanFixture(t, "options-remove-slot@north.test")
	plan := f.lunchWithOption(t)

	if err := f.svc.RemoveMeal(f.ctx, plan.Days[0].Meals[0].ID, f.userID); err != nil {
		t.Fatalf("remove default: %v", err)
	}
	stored := f.reload(t, plan.ID)
	if len(stored.Days[0].Meals) != 0 {
		t.Fatalf("meals = %+v, want the whole slot gone", stored.Days[0].Meals)
	}
	if stored.TotalMacros.ProteinG != 0 {
		t.Fatalf("plan protein = %v, want 0", stored.TotalMacros.ProteinG)
	}
}

func TestLogMealOnAnAlternativeLogsThatOption(t *testing.T) {
	f := newPlanFixture(t, "options-log@north.test")
	plan := f.lunchWithOption(t)
	alt := plan.Days[0].Meals[0].Alternatives[0]

	entry, err := meals.NewFoodLogService(meals.NewRepository(f.pool)).LogMeal(f.ctx, f.userID, meals.LogMealInput{
		MealID: alt.ID, LogDate: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("log alternative: %v", err)
	}
	if entry.Label != "Almoço · Opção 2" {
		t.Fatalf("label = %q", entry.Label)
	}
	if entry.MealID == nil || *entry.MealID != alt.ID || !within(entry.Macros.ProteinG, 310, 0.001) {
		t.Fatalf("entry = %+v, want the alternative's 310 g protein", entry)
	}
}

func TestApplyChangesEditsOptions(t *testing.T) {
	f := newPlanFixture(t, "options-changes@north.test")
	plan := f.lunchWithOption(t)

	stored, applied, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpAddOption, Meal: "almoço", OptionLabel: "Opção 3"},
		{Op: meals.OpAddFood, Meal: "almoço", Option: 3, IngredientID: f.rice.ID, Grams: 200},
		{Op: meals.OpSetGrams, Meal: "almoço", Option: 2, Food: "chicken", Grams: 500},
		{Op: meals.OpAddFood, Meal: "almoço", Option: 2, IngredientID: f.rice.ID, Grams: 100},
	}, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	slot := stored.Days[0].Meals[0]
	if len(slot.Alternatives) != 2 {
		t.Fatalf("alternatives = %+v", slot.Alternatives)
	}
	two, three := slot.Alternatives[0], slot.Alternatives[1]
	if three.OptionLabel != "Opção 3" || len(three.Ingredients) != 1 || three.Ingredients[0].IngredientID != f.rice.ID {
		t.Fatalf("option 3 = %+v", three)
	}
	if len(two.Ingredients) != 2 || two.Ingredients[0].QuantityGrams != 500 || two.Ingredients[1].IngredientID != f.rice.ID {
		t.Fatalf("option 2 = %+v", two.Ingredients)
	}
	if len(slot.Ingredients) != 1 || slot.Ingredients[0].QuantityGrams != 100 {
		t.Fatalf("the default changed: %+v", slot.Ingredients)
	}
	if got := stored.TotalMacros.ProteinG; !within(got, 31, 0.001) {
		t.Fatalf("plan protein = %v, want 31", got)
	}
	if got := applied[0].String(); !strings.Contains(got, "Almoço · Opção 3") {
		t.Fatalf("add_option summary = %q", got)
	}
	if got := applied[3].String(); got != "added 100 g White rice to Almoço · Opção 2 on Monday" {
		t.Fatalf("add_food summary = %q", got)
	}

	stored, _, err = f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpRemoveOption, Meal: "almoço", Option: 2},
	}, false)
	if err != nil {
		t.Fatalf("remove option: %v", err)
	}
	slot = stored.Days[0].Meals[0]
	if len(slot.Alternatives) != 1 || slot.Alternatives[0].OptionLabel != "Opção 3" || len(slot.Ingredients) != 1 {
		t.Fatalf("after remove_option = %+v", slot)
	}
}

func TestApplyChangesRefusesAnOptionTheSlotLacks(t *testing.T) {
	f := newPlanFixture(t, "options-range@north.test")
	plan := f.lunchWithOption(t)

	_, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpAddFood, Meal: "almoço", Option: 3, IngredientID: f.rice.ID, Grams: 50},
	}, false)
	if !apperr.Is(err, apperr.ErrValidation) || !strings.Contains(err.Error(), "Almoço on Monday has 2 options") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range []meals.PlanChange{
		{Op: meals.OpRemoveOption, Meal: "almoço", Option: 1},
		{Op: meals.OpRemoveOption, Meal: "almoço"},
		{Op: meals.OpAddOption, Meal: "almoço"},
		{Op: meals.OpAddOption, Meal: "almoço", OptionLabel: strings.Repeat("x", meals.MaxOptionLabelRunes+1)},
		{Op: meals.OpRemoveMeal, Meal: "almoço", Option: 2},
	} {
		if _, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{c}, false); !apperr.Is(err, apperr.ErrValidation) {
			t.Fatalf("%+v err = %v", c, err)
		}
	}
	if got := f.reload(t, plan.ID).Days[0].Meals[0].Alternatives; len(got) != 1 {
		t.Fatalf("a refused change wrote: %+v", got)
	}
}

func TestApplyChangesRemoveMealRemovesEveryOption(t *testing.T) {
	f := newPlanFixture(t, "options-remove-meal@north.test")
	plan := f.lunchWithOption(t)

	stored, _, err := f.svc.ApplyChanges(f.ctx, plan.ID, f.userID, []meals.PlanChange{
		{Op: meals.OpRemoveMeal, Meal: "almoço"},
	}, false)
	if err != nil {
		t.Fatalf("remove meal: %v", err)
	}
	if len(stored.Days[0].Meals) != 0 {
		t.Fatalf("meals = %+v", stored.Days[0].Meals)
	}
}

func TestSharedNamesListsTheSharedCatalogOnly(t *testing.T) {
	f := newPlanFixture(t, "options-shared-names@north.test")
	ingredients := meals.NewIngredientService(meals.NewRepository(f.pool))
	private, err := ingredients.Create(f.ctx, f.userID, meals.IngredientInput{
		Name: "Avó's private stew", Category: meals.CategoryOther,
		Per100g: meals.Macros{Calories: 100, ProteinG: 5, FatG: 5, CarbG: 10},
	})
	if err != nil {
		t.Fatalf("create private ingredient: %v", err)
	}

	names, err := ingredients.SharedNames(f.ctx)
	if err != nil {
		t.Fatalf("shared names: %v", err)
	}
	if !slices.Contains(names, "Chicken breast") {
		t.Fatalf("the seeded catalog is missing from %d names", len(names))
	}
	if slices.Contains(names, private.Name) {
		t.Fatal("a private ingredient is in the shared names")
	}
}
