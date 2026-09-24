package meals

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestNutritionShapes(t *testing.T) {
	t.Parallel()

	oats := IngredientView{
		ID: uuid.MustParse("11111111-aaaa-aaaa-aaaa-111111111111"), Name: "Rolled oats", Category: "grains",
		Own: false, ServingSizeGrams: 40, Per100g: MacrosView{Calories: 379, ProteinG: 13.2, FatG: 6.5, CarbG: 67.7},
	}
	apitest.AssertGolden(t, "nutrition-ingredients.golden.json", IngredientList{Ingredients: []IngredientView{oats}})

	portion := MealIngredientView{
		ID: uuid.MustParse("22222222-aaaa-aaaa-aaaa-222222222222"), IngredientID: oats.ID, Name: oats.Name,
		QuantityGrams: 80, Macros: MacrosView{Calories: 303.2, ProteinG: 10.6, FatG: 5.2, CarbG: 54.2},
	}
	summary := PlanSummary{
		ID: uuid.MustParse("33333333-aaaa-aaaa-aaaa-333333333333"), Name: "Training days", Description: "High carb",
		TotalMacros: portion.Macros, MealCount: 1,
	}
	apitest.AssertGolden(t, "nutrition-plans.golden.json", PlanList{Plans: []PlanSummary{summary}})
	apitest.AssertGolden(t, "nutrition-plan.golden.json", PlanDetail{
		PlanSummary: summary, Objective: "maintain", ActivityLevel: "active",
		Gender: "female", Meals: []MealView{{
			ID: uuid.MustParse("44444444-aaaa-aaaa-aaaa-444444444444"), MealNumber: 1, Name: "Breakfast",
			TotalMacros: portion.Macros, Ingredients: []MealIngredientView{portion},
		}},
	})

	grams := 80.0
	apitest.AssertGolden(t, "nutrition-log.golden.json", FoodLog{
		Date: "2026-09-24",
		Entries: []FoodLogEntryView{{
			ID: uuid.MustParse("55555555-aaaa-aaaa-aaaa-555555555555"), Label: "Rolled oats", QuantityGrams: &grams,
			Macros: portion.Macros, LoggedAt: time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC),
		}},
		Totals: portion.Macros,
		Progress: &NutritionProgress{
			Goal: MacrosView{Calories: 2200, ProteinG: 130, FatG: 70, CarbG: 260}, Logged: portion.Macros,
			Summary: "1897 kcal under your goal", Recommendation: "Your goal fits a maintenance phase.",
		},
	})
}
