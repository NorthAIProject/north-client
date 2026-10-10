package planimport

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/planimport/draft"
)

// The review page names each option's fields the way the form reader parses
// them back: o0 the first option, oN the Nth alternative.
func TestMealDayRendersEachOptionWithItsOwnFields(t *testing.T) {
	t.Parallel()

	day := draft.MealDayDraft{Label: "Every day", Meals: []draft.MealDraftMeal{{
		Name: "Almoço", OptionLabel: "Prato – carne",
		Foods: []draft.FoodDraft{{Food: "Chicken", SourceText: "125 g frango"}},
		Alternatives: []draft.MealDraftOption{
			{Label: "", Foods: []draft.FoodDraft{{Food: "Hake", Estimated: true}}},
		},
	}}}
	var b strings.Builder
	if err := mealDay(0, day, false, true).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, want := range []string{
		`name="d0.m0.o0.f0.grams"`, `name="d0.m0.o1.f0.grams"`,
		`value="delete-option:0:0:1"`, `value="delete-food:0:0:1:0"`,
		"Prato – carne", "Option 2", "counted", "estimated", "125 g frango", "Every day",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered day lacks %q", want)
		}
	}
}
