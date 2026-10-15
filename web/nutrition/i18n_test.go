package nutrition

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func renderIn(t *testing.T, locale string, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(i18n.WithLocale(context.Background(), locale), &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

// A plan's day card is where the person spends their time: its weekday, the
// macro bars and the add-food line all have to be in their language, not
// only the headings.
func TestDayCardRendersInTheChosenLanguage(t *testing.T) {
	target := &meal.Macros{Calories: 2000, ProteinG: 150, CarbG: 200, FatG: 70}
	day := meal.Day{
		ID: uuid.New(), Weekday: time.Monday,
		Meals: []meal.Meal{{ID: uuid.New(), Name: "Desayuno", OptionIndex: 1}},
	}
	page := PlanPage{
		Plan:   meal.MealPlan{ID: uuid.New(), Name: "Volumen", Days: []meal.Day{day, {ID: uuid.New(), Weekday: time.Tuesday}}},
		Target: target,
	}
	status := &meal.DayStatus{Target: *target, Remaining: *target}

	html := renderIn(t, "es", dayCard(page, day, status))
	for _, want := range []string{"Lunes", "Proteína", "Carbohidratos", "Este día:", "quedan", "Añadir comida", "Quitar día", "¿Quitar el lunes?", "Desayuno"} {
		if !strings.Contains(html, want) {
			t.Errorf("es: missing %q", want)
		}
	}
	for _, english := range []string{"Monday", "Add meal", "Remove day", "This day:", "g left"} {
		if strings.Contains(html, english) {
			t.Errorf("es: still shows English %q", english)
		}
	}
}

func TestIngredientCategoriesAreTranslated(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), "pt-PT")
	if got := categoryLabel(ctx, meal.CategoryDairy); got != "Laticínios" {
		t.Errorf("pt-PT dairy = %q", got)
	}
	if got := categoryLabel(ctx, "unheard-of"); got != "Outro" {
		t.Errorf("an unknown category should read as Other, got %q", got)
	}
}

// The live preview is built in the browser from catalogue strings, so its
// placeholders have to survive translation in every language.
func TestPreviewTextCarriesTranslatedWords(t *testing.T) {
	for _, locale := range []string{"en", "es", "pt-PT", "pt-BR"} {
		js := previewText(i18n.WithLocale(context.Background(), locale))
		for _, placeholder := range []string{"{list}", "{m}", "{g}"} {
			if !strings.Contains(js, placeholder) {
				t.Errorf("%s: preview lost the %s placeholder", locale, placeholder)
			}
		}
	}
	if js := previewText(i18n.WithLocale(context.Background(), "pt-BR")); !strings.Contains(js, "Depois disso") || !strings.Contains(js, "carboidratos") {
		t.Errorf("pt-BR preview is not in Portuguese: %s", js)
	}
}
