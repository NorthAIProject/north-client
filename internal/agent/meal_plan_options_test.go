package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestPlanChangeFromArgCarriesTheOption(t *testing.T) {
	c, err := planChangeFromArg(changeArg{Op: "add_food", Meal: "Lunch", Food: "rice", Grams: 80, Option: 2})
	if err != nil || c.Option != 2 {
		t.Fatalf("add_food on option 2 = %+v, %v", c, err)
	}

	// add_option names the new option and adds no food.
	c, err = planChangeFromArg(changeArg{Op: "add_option", Meal: "Lunch", OptionLabel: "  Peixe ", Food: "rice", Grams: 10})
	if err != nil || c.Op != meals.OpAddOption || c.OptionLabel != "Peixe" || c.Food != "" || c.Grams != 0 {
		t.Fatalf("add_option = %+v, %v", c, err)
	}

	c, err = planChangeFromArg(changeArg{Op: "remove_option", Meal: "Lunch", Option: 3})
	if err != nil || c.Op != meals.OpRemoveOption || c.Option != 3 {
		t.Fatalf("remove_option = %+v, %v", c, err)
	}

	if _, err = planChangeFromArg(changeArg{Op: "add_food", Meal: "Lunch", Food: "rice", Grams: 80, Option: -1}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("a negative option err = %v", err)
	}
}

// optionPlan is two identical days whose lunch has three options.
func optionPlan() meals.MealPlan {
	day := func(wd time.Weekday) meal.Day {
		return meal.Day{
			Weekday: wd,
			Meals: []meal.Meal{
				{Name: "Breakfast", TotalMacros: meal.Macros{Calories: 300}, Ingredients: []meal.MealIngredient{{IngredientName: "Oats", QuantityGrams: 80}}},
				{
					Name: "Almoço", OptionLabel: "Frango", TotalMacros: meal.Macros{Calories: 620},
					Ingredients: []meal.MealIngredient{{IngredientName: "Chicken", QuantityGrams: 150}},
					Alternatives: []meal.Meal{
						{
							Name: "Almoço", OptionIndex: 2, OptionLabel: "Peixe", TotalMacros: meal.Macros{Calories: 580},
							Ingredients: []meal.MealIngredient{{IngredientName: "Hake", QuantityGrams: 200, Estimated: true}},
						},
						{Name: "Almoço", OptionIndex: 3, OptionLabel: "Peixe grelhado", TotalMacros: meal.Macros{Calories: 0}},
					},
				},
			},
		}
	}
	return meals.MealPlan{
		Name:     "Dieta",
		Settings: meal.PlanSettings{Type: meal.MidCarb, Mode: meal.Advanced},
		Days:     []meal.Day{day(time.Monday), day(time.Tuesday)},
	}
}

func TestTodaysMealPicksAnOption(t *testing.T) {
	plan := optionPlan()

	for option, want := range map[string]string{
		"":       "Frango",
		"1":      "Frango",
		"2":      "Peixe",
		" 2 ":    "Peixe",
		"frango": "Frango",
		// An exact label wins over a longer one that contains it.
		"PEIXE": "Peixe",
	} {
		m, err := todaysMeal(plan, time.Monday, "almoço", option)
		if err != nil || m.OptionLabel != want {
			t.Errorf("option %q = %q, %v; want %q", option, m.OptionLabel, err, want)
		}
	}

	if _, err := todaysMeal(plan, time.Monday, "almoço", "4"); !apperr.Is(err, apperr.ErrNotFound) ||
		!strings.Contains(err.Error(), "3 options") {
		t.Errorf("option 4 err = %v, want not found naming the 3 options", err)
	}
	if _, err := todaysMeal(plan, time.Monday, "almoço", "carne"); !apperr.Is(err, apperr.ErrNotFound) ||
		!strings.Contains(err.Error(), `2 "Peixe"`) {
		t.Errorf("unknown label err = %v, want not found listing the options", err)
	}
	if _, err := todaysMeal(plan, time.Monday, "almoço", "e"); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("ambiguous label err = %v, want a validation error", err)
	}
	// Option 3 has no food yet, by number or by part of its label.
	for _, option := range []string{"3", "grel"} {
		if _, err := todaysMeal(plan, time.Monday, "almoço", option); !apperr.Is(err, apperr.ErrValidation) ||
			!strings.Contains(err.Error(), "Peixe grelhado") {
			t.Errorf("option %q: an empty option was loggable: %v", option, err)
		}
	}
}

func TestDescribeMealPlanListsOptionsAndCollapsesIdenticalDays(t *testing.T) {
	plan := optionPlan()
	var b strings.Builder
	describeMealPlan(&b, plan, nil)
	got := b.String()

	for _, want := range []string{
		"Every day (Monday, Tuesday): 920 kcal",
		"\n  Breakfast (300 kcal): 80 g Oats",
		`Almoço (option 1 "Frango", counted, 620 kcal): 150 g Chicken`,
		`    option 2 "Peixe" (580 kcal): 200 g Hake (estimated)`,
		`    option 3 "Peixe grelhado" (0 kcal): nothing yet`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("description lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\nMonday:") || strings.Count(got, "Breakfast") != 1 {
		t.Errorf("identical days were not described once:\n%s", got)
	}

	// A full week reads as Monday–Sunday.
	week := optionPlan()
	week.Days = nil
	for _, wd := range meal.WeekOrder {
		d := plan.Days[0]
		d.Weekday = wd
		week.Days = append(week.Days, d)
	}
	b.Reset()
	describeMealPlan(&b, week, nil)
	if !strings.Contains(b.String(), "Every day (Monday–Sunday):") {
		t.Errorf("a full identical week was not named Monday–Sunday:\n%s", b.String())
	}

	// One day different: every day is listed by its weekday.
	plan.Days[1].Meals[0].Ingredients[0].QuantityGrams = 60
	b.Reset()
	describeMealPlan(&b, plan, nil)
	got = b.String()
	if !strings.Contains(got, "\nMonday: ") || !strings.Contains(got, "\nTuesday: ") || strings.Contains(got, "Every day") {
		t.Errorf("different days were collapsed:\n%s", got)
	}
}
