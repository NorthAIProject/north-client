package meals

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	nutritionpages "github.com/NorthAIProject/north-client/web/nutrition"
)

func pickerDay(wd time.Weekday, meals ...meal.Meal) meal.Day {
	return meal.Day{ID: uuid.New(), Weekday: wd, Meals: meals}
}

func pickerMeal(name, label string, alternatives ...meal.Meal) meal.Meal {
	return meal.Meal{ID: uuid.New(), Name: name, OptionLabel: label, Alternatives: alternatives}
}

func pickerLabels(opts []nutritionpages.MealOption) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Label)
	}
	return out
}

func TestMealPickerListsTodaysDayWithEveryOption(t *testing.T) {
	alt := pickerMeal("Almoço", "Opção 2")
	everyDay := meal.MealPlan{Name: "Plano", Days: []meal.Day{
		pickerDay(time.Monday, pickerMeal("Almoço", "Opção 1", pickerMeal("Almoço", "Opção 3"))),
		pickerDay(time.Tuesday, pickerMeal("Pequeno-almoço", ""), pickerMeal("Almoço", "Opção 1", alt)),
		pickerDay(time.Wednesday, pickerMeal("Jantar", "")),
	}}

	got := mealPickerOptions([]meal.MealPlan{everyDay}, time.Tuesday)

	labels := pickerLabels(got)
	want := []string{"Plano – Pequeno-almoço", "Plano – Almoço · Opção 1", "Plano – Almoço · Opção 2"}
	if !slices.Equal(labels, want) {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
	if got[2].ID != alt.ID.String() {
		t.Fatalf("option 2 logs meal %s, want its own row %s", got[2].ID, alt.ID)
	}
}

func TestMealPickerFallsBackToEveryDayWhenAPlanHasNoneToday(t *testing.T) {
	short := meal.MealPlan{Name: "Short", Days: []meal.Day{
		pickerDay(time.Monday, pickerMeal("Lunch", "")),
		pickerDay(time.Tuesday, pickerMeal("Dinner", "")),
	}}

	got := mealPickerOptions([]meal.MealPlan{short}, time.Saturday)

	labels := pickerLabels(got)
	want := []string{"Short – Mon – Lunch", "Short – Tue – Dinner"}
	if !slices.Equal(labels, want) {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
}
