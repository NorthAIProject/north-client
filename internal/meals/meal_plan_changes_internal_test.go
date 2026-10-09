package meals

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestPickMealPrefersAnExactName(t *testing.T) {
	day := meal.Day{Weekday: time.Monday, Meals: []meal.Meal{
		{Name: "Snack"}, {Name: "Afternoon snack"}, {Name: "Lunch"}, {Name: "Late lunch"},
	}}

	if i, found, err := PickMeal(day, "SNACK"); err != nil || !found || i != 0 {
		t.Fatalf("exact = %d, %t, %v", i, found, err)
	}
	if i, found, err := PickMeal(day, "afternoon"); err != nil || !found || i != 1 {
		t.Fatalf("part = %d, %t, %v", i, found, err)
	}
	if _, found, err := PickMeal(day, "dinner"); err != nil || found {
		t.Fatalf("missing = %t, %v", found, err)
	}
	day.Meals = day.Meals[1:]
	if _, _, err := PickMeal(day, "un"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("ambiguous err = %v", err)
	}
}

func TestPickPortionsFindsEveryPortionOfOneFood(t *testing.T) {
	rice, cakes, chicken := uuid.New(), uuid.New(), uuid.New()
	m := meal.Meal{Ingredients: []meal.MealIngredient{
		{IngredientID: rice, IngredientName: "White rice"},
		{IngredientID: chicken, IngredientName: "Chicken breast"},
		{IngredientID: rice, IngredientName: "White rice"},
		{IngredientID: cakes, IngredientName: "Rice cakes"},
	}}

	if got, err := pickPortions(m, uuid.Nil, "white rice"); err != nil || len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("by name = %v, %v", got, err)
	}
	if got, err := pickPortions(m, chicken, ""); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("by id = %v, %v", got, err)
	}
	if got, err := pickPortions(m, uuid.Nil, "salmon"); err != nil || len(got) != 0 {
		t.Fatalf("missing = %v, %v", got, err)
	}
	if _, err := pickPortions(m, uuid.Nil, "rice"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("ambiguous err = %v", err)
	}
}
