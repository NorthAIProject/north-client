package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestParseWeekdayTakesNamesAndAbbreviations(t *testing.T) {
	for name, want := range map[string]time.Weekday{
		"Monday": time.Monday, "tue": time.Tuesday, " THURSDAY ": time.Thursday,
		"sa": time.Saturday, "su": time.Sunday, "wed": time.Wednesday,
	} {
		got, ok := parseWeekday(name)
		if !ok || got != want {
			t.Errorf("parseWeekday(%q) = %v, %t; want %v", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "t", "s", "funday", "weekend"} {
		if _, ok := parseWeekday(name); ok {
			t.Errorf("parseWeekday(%q) matched", name)
		}
	}
}

func TestParseWeekdaysDropsRepeatsAndRefusesUnknowns(t *testing.T) {
	got, err := parseWeekdays([]string{"Mon", "monday", "Fri"})
	if err != nil || len(got) != 2 || got[0] != time.Monday || got[1] != time.Friday {
		t.Fatalf("parseWeekdays = %v, %v", got, err)
	}
	if got, err := parseWeekdays(nil); err != nil || len(got) != 0 {
		t.Fatalf("no days = %v, %v; want every day (empty)", got, err)
	}
	if _, err := parseWeekdays([]string{"Mon", "someday"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("unknown weekday err = %v", err)
	}
}

func TestPlanChangeFromArg(t *testing.T) {
	c, err := planChangeFromArg(changeArg{Op: "add_food", Days: []string{"Tue"}, Meal: " Breakfast ", Food: "oats", Grams: 30})
	if err != nil {
		t.Fatalf("add_food: %v", err)
	}
	if c.Op != meals.OpAddFood || c.Meal != "Breakfast" || c.Food != "oats" || c.Grams != 30 ||
		len(c.Days) != 1 || c.Days[0] != time.Tuesday || c.IngredientID != uuid.Nil {
		t.Fatalf("add_food = %+v", c)
	}

	// A meal op ignores food and grams the model filled in anyway.
	c, err = planChangeFromArg(changeArg{Op: "remove_meal", Meal: "Snack", Food: "nuts", Grams: 10})
	if err != nil || c.Food != "" || c.Grams != 0 || len(c.Days) != 0 {
		t.Fatalf("remove_meal = %+v, %v", c, err)
	}

	for name, bad := range map[string]changeArg{
		"unknown op":           {Op: "rename_meal", Meal: "Lunch"},
		"no meal":              {Op: "add_meal"},
		"add_food without one": {Op: "add_food", Meal: "Lunch", Grams: 50},
		"add_food no grams":    {Op: "add_food", Meal: "Lunch", Food: "rice"},
		"set_grams too much":   {Op: "set_grams", Meal: "Lunch", Food: "rice", Grams: 5000},
		"remove_food no food":  {Op: "remove_food", Meal: "Lunch"},
		"bad weekday":          {Op: "add_meal", Meal: "Lunch", Days: []string{"Caturday"}},
	} {
		if _, err := planChangeFromArg(bad); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}

func TestPlanChangesFromArgsNamesTheBadChange(t *testing.T) {
	_, err := planChangesFromArgs([]changeArg{
		{Op: "add_meal", Meal: "Lunch"},
		{Op: "set_grams", Meal: "Lunch", Food: "rice"},
	})
	if err == nil || !strings.Contains(err.Error(), "change 2") {
		t.Fatalf("err = %v, want it to name change 2", err)
	}
	if _, err := planChangesFromArgs(nil); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("no changes err = %v", err)
	}
}

func TestPickMealPlan(t *testing.T) {
	now := time.Now()
	cut := meals.MealPlan{ID: uuid.New(), Name: "Cutting", UpdatedAt: now.Add(-time.Hour)}
	bulk := meals.MealPlan{ID: uuid.New(), Name: "Winter bulk", UpdatedAt: now}
	list := []meals.MealPlan{cut, bulk}

	if got, err := pickMealPlan(list, "", false); err != nil || got.ID != bulk.ID {
		t.Fatalf("unnamed read = %q, %v; want the most recently changed", got.Name, err)
	}
	if _, err := pickMealPlan(list, "", true); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("unnamed write with two plans err = %v", err)
	}
	if got, err := pickMealPlan([]meals.MealPlan{cut}, "", true); err != nil || got.ID != cut.ID {
		t.Fatalf("unnamed write with one plan = %q, %v", got.Name, err)
	}
	if got, err := pickMealPlan(list, "bulk", true); err != nil || got.ID != bulk.ID {
		t.Fatalf("part of a name = %q, %v", got.Name, err)
	}
	if _, err := pickMealPlan(list, "maintenance", false); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("unknown name err = %v", err)
	}
	if _, err := pickMealPlan(nil, "", false); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("no plans err = %v", err)
	}
}

func TestTodaysMeal(t *testing.T) {
	plan := meals.MealPlan{Name: "Cutting", Days: []meal.Day{{
		Weekday: time.Monday,
		Meals: []meal.Meal{
			{Name: "Breakfast", Ingredients: []meal.MealIngredient{{IngredientName: "Oats", QuantityGrams: 50}}},
			{Name: "Afternoon snack"},
		},
	}}}

	if m, err := todaysMeal(plan, time.Monday, "breakfast"); err != nil || m.Name != "Breakfast" {
		t.Fatalf("breakfast = %+v, %v", m, err)
	}
	if _, err := todaysMeal(plan, time.Monday, "snack"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("an empty meal was loggable: %v", err)
	}
	if _, err := todaysMeal(plan, time.Monday, "dinner"); !apperr.Is(err, apperr.ErrNotFound) ||
		!strings.Contains(err.Error(), `"Breakfast"`) {
		t.Fatalf("missing meal err = %v, want it to list the day's meals", err)
	}
	if _, err := todaysMeal(plan, time.Tuesday, "breakfast"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("unplanned day err = %v", err)
	}
}

func TestExplainOverageKeepsTheConflict(t *testing.T) {
	over := &meals.OverageError{Verdict: meal.Verdict{CanConfirm: true, Over: []meal.DayOverage{{
		Weekday: time.Tuesday, Status: meal.DayStatus{Over: meal.Macros{CarbG: 20}},
	}}}}
	err := explainOverage(over)
	if !apperr.Is(err, apperr.ErrConflict) || !strings.Contains(err.Error(), "allow_over_target") ||
		!strings.Contains(err.Error(), "Tuesday would be 20 g over on carbs") {
		t.Fatalf("err = %v", err)
	}
	plain := errors.New("boom")
	if explainOverage(plain) != plain {
		t.Fatal("a non-overage error was changed")
	}
}
