package nutrition

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// lunchSlot is a lunch whose default is unlabelled, with a labelled second
// option holding one estimated, imported line.
func lunchSlot() meal.Meal {
	return meal.Meal{
		ID: uuid.New(), Name: "Almoço", OptionIndex: 1,
		Alternatives: []meal.Meal{{
			ID: uuid.New(), Name: "Almoço", OptionIndex: 2, OptionLabel: "Opção 2",
			Ingredients: []meal.MealIngredient{{
				ID: uuid.New(), IngredientName: "Chicken breast", QuantityGrams: 150,
				SourceText: "frango à vontade", Estimated: true,
			}},
		}},
	}
}

func TestMealBlockShowsOptionsAsTabs(t *testing.T) {
	slot := lunchSlot()
	alt := slot.Alternatives[0]
	html := render(t, mealBlock(PlanPage{}, slot, &meal.DayStatus{}))

	for _, want := range []string{
		"data-tui-tabs",
		"Option 1", // the unlabelled default
		"Opção 2",
		"Counted",
		"Remove meal (all options)",
		"Remove option",
		// Each option keeps its own routes, keyed by its own meal row.
		"/app/nutrition/meals/" + slot.ID.String() + "/ingredients",
		"/app/nutrition/meals/" + alt.ID.String() + "/ingredients",
		"/app/nutrition/meals/" + alt.ID.String() + "/delete",
		"/app/nutrition/meals/" + slot.ID.String() + "/options",
		// The imported line keeps what the file said, flagged as a guess.
		"frango à vontade",
		"estimated",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := strings.Count(html, "Counted"); n != 1 {
		t.Errorf("Counted appears %d times, want once (option 1 only)", n)
	}
}

func TestMealBlockWithoutOptionsHasNoTabs(t *testing.T) {
	slot := meal.Meal{ID: uuid.New(), Name: "Jantar", OptionIndex: 1}
	html := render(t, mealBlock(PlanPage{}, slot, &meal.DayStatus{}))

	if strings.Contains(html, "data-tui-tabs") || strings.Contains(html, "Counted") {
		t.Error("a slot with one option should render without tabs")
	}
	for _, want := range []string{"Remove meal", "Add option", "/app/nutrition/meals/" + slot.ID.String() + "/options"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(html, "all options") {
		t.Error("a single-option slot should not offer to remove all options")
	}
}

func TestMealBlockOpensTheOptionWithAProblem(t *testing.T) {
	slot := lunchSlot()
	alt := slot.Alternatives[0]
	page := PlanPage{Problem: Problem{Scope: "portion:" + alt.ID.String(), Errors: map[string]string{"quantity_grams": "Give an amount."}}}
	html := render(t, mealBlock(page, slot, &meal.DayStatus{}))

	active := `data-tui-tabs-value="` + alt.ID.String() + `" data-tui-tabs-state="active"`
	if !strings.Contains(html, active) {
		t.Errorf("the option whose form was refused should be the open tab")
	}
}

func TestPlanNotesAreEscapedAndKeepTheirLines(t *testing.T) {
	html := render(t, planNotes("Beber 2 L de água.\n<b>Sem</b> açúcar."))

	for _, want := range []string{"From your plan", "whitespace-pre-wrap", "Beber 2 L de água.\n&lt;b&gt;Sem&lt;/b&gt; açúcar."} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
}
