package planimport

import (
	"context"
	"slices"
	"testing"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// What a person reviews is in their language; what the coach is told about
// an import stays English, because the coach's import runs without a locale.
func TestOverLinesAndFlagsReadTheLocale(t *testing.T) {
	t.Parallel()

	ctx := i18n.WithLocale(context.Background(), "pt-PT")
	if got := overList(ctx, meal.Macros{CarbG: 12.2}); !slices.Equal(got, []string{"hidratos: 13 g a mais"}) {
		t.Fatalf("pt-PT over = %q", got)
	}
	if got := overList(context.Background(), meal.Macros{CarbG: 12.2}); !slices.Equal(got, []string{"carbs over by 13 g"}) {
		t.Fatalf("English over = %q", got)
	}

	food := foodFromRow("es", MealRow{Food: "arroz", Quantity: "mucho", Protein: "x"})
	for _, want := range []string{`La cantidad "mucho" no es un número.`, `Proteína: "x" no es una cantidad en gramos.`} {
		if !slices.Contains(food.Flags, want) {
			t.Errorf("es flags = %q, want %q among them", food.Flags, want)
		}
	}
}

func TestUnnamedOptionsAreNumberedInTheLocale(t *testing.T) {
	t.Parallel()

	d, err := buildMeal("es", MealReading{Name: "Semana", Rows: []MealRow{
		{Day: "Lunes", Meal: "Almuerzo", Option: "A", Food: "arroz", Quantity: "100 g"},
		{Day: "Lunes", Meal: "Almuerzo", Option: "", Food: "pasta", Quantity: "100 g"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	m := d.Days[0].Meals[0]
	if len(m.Alternatives) != 1 || m.Alternatives[0].Label != "Opción 2" {
		t.Fatalf("alternatives = %+v, want one named in Spanish", m.Alternatives)
	}
	if got := optionName("pt-BR", MealDraftMeal{Alternatives: m.Alternatives}, 0); got != "Opção 1" {
		t.Errorf("unnamed first option = %q", got)
	}
}
