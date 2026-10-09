package meal

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// lunchSlot is a default lunch with two alternatives, the second labelled.
func lunchSlot() Meal {
	return Meal{
		ID: uuid.New(), MealNumber: 2, Name: "Almoço", OptionIndex: 1,
		TotalMacros: Macros{ProteinG: 40},
		Alternatives: []Meal{
			{ID: uuid.New(), MealNumber: 2, Name: "Almoço", OptionIndex: 2, OptionLabel: "Opção 2", TotalMacros: Macros{ProteinG: 400}},
			{ID: uuid.New(), MealNumber: 2, Name: "Almoço", OptionIndex: 4, OptionLabel: "Opção 3", TotalMacros: Macros{ProteinG: 900}},
		},
	}
}

func TestOptionsListTheDefaultThenItsAlternatives(t *testing.T) {
	slot := lunchSlot()
	got := slot.Options()
	if len(got) != 3 || got[0].ID != slot.ID || got[1].ID != slot.Alternatives[0].ID || got[2].ID != slot.Alternatives[1].ID {
		t.Fatalf("options = %+v", got)
	}
	if one := (Meal{Name: "Snack"}).Options(); len(one) != 1 || one[0].Name != "Snack" {
		t.Fatalf("a slot with no alternatives = %+v", one)
	}
}

func TestDisplayNameAddsTheOptionLabel(t *testing.T) {
	slot := lunchSlot()
	if got := slot.DisplayName(); got != "Almoço" {
		t.Fatalf("default = %q", got)
	}
	if got := slot.Alternatives[0].DisplayName(); got != "Almoço · Opção 2" {
		t.Fatalf("alternative = %q", got)
	}
}

func TestLocateMealFindsDefaultsAndAlternatives(t *testing.T) {
	breakfast := Meal{ID: uuid.New(), MealNumber: 1, Name: "Breakfast", OptionIndex: 1}
	slot := lunchSlot()
	plan := MealPlan{Days: []Day{
		{Weekday: time.Monday, Meals: []Meal{breakfast}},
		{Weekday: time.Tuesday, Meals: []Meal{slot}},
	}}

	if day, alt, ok := plan.LocateMeal(breakfast.ID); !ok || alt || day != 0 {
		t.Fatalf("default = %d, %t, %t", day, alt, ok)
	}
	if day, alt, ok := plan.LocateMeal(slot.Alternatives[1].ID); !ok || !alt || day != 1 {
		t.Fatalf("alternative = %d, %t, %t", day, alt, ok)
	}
	if _, _, ok := plan.LocateMeal(uuid.New()); ok {
		t.Fatal("found a meal the plan does not have")
	}
	if day, ok := plan.DayIndexOfMeal(slot.Alternatives[0].ID); !ok || day != 1 {
		t.Fatalf("DayIndexOfMeal(alternative) = %d, %t", day, ok)
	}
}

func TestConsumedCountsOnlyDefaults(t *testing.T) {
	day := Day{Meals: []Meal{lunchSlot(), {TotalMacros: Macros{ProteinG: 10}}}}
	if got := day.Consumed().ProteinG; got != 50 {
		t.Fatalf("consumed protein = %v, want 50 (the alternatives' 1300 left out)", got)
	}
}
