package meals

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
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
	carbPct := 35.5
	summary := PlanSummary{
		ID: uuid.MustParse("33333333-aaaa-aaaa-aaaa-333333333333"), Name: "Training days", Description: "High carb",
		PlanType: meal.HighCarb, Mode: meal.Advanced, DayCount: 2, TotalMacros: portion.Macros,
	}
	apitest.AssertGolden(t, "nutrition-plans.golden.json", PlanList{Plans: []PlanSummary{summary}})

	target := MacrosView{Calories: 2190, ProteinG: 130, FatG: 70, CarbG: 260}
	lowCarb := meal.LowCarb
	apitest.AssertGolden(t, "nutrition-plan.golden.json", PlanDetail{
		PlanSummary: summary, Objective: "maintain", ActivityLevel: "active", Gender: "female", Target: &target,
		Days: []PlanDayView{
			{
				ID: uuid.MustParse("66666666-aaaa-aaaa-aaaa-666666666666"), Weekday: 1, CarbType: &lowCarb,
				Status: &DayStatusView{
					Target:    MacrosView{Calories: 1331.2, ProteinG: 130, FatG: 70, CarbG: 40.3},
					Consumed:  portion.Macros,
					Remaining: MacrosView{Calories: 1028, ProteinG: 119.4, FatG: 64.8, CarbG: -13.9},
					Over:      MacrosView{CarbG: 13.9},
					IsOver:    true,
				},
				Meals: []MealView{{
					ID: uuid.MustParse("44444444-aaaa-aaaa-aaaa-444444444444"), MealNumber: 1, Name: "Breakfast",
					TotalMacros: portion.Macros, Ingredients: []MealIngredientView{portion},
				}},
			},
			{ID: uuid.MustParse("77777777-aaaa-aaaa-aaaa-777777777777"), Weekday: 6, Meals: []MealView{}},
		},
	})

	defaultCarbG := 92.3
	apitest.AssertGolden(t, "nutrition-plan-options.golden.json", PlanOptions{
		Target: &target, MaxDays: 7,
		PlanTypes: []PlanTypeOption{
			{ID: meal.MidCarb, MinPct: 26, MaxPct: 45, DefaultPct: &carbPct, DefaultCarbG: &defaultCarbG},
			{ID: meal.Custom, MinPct: 0, MaxPct: 100, AdvancedOnly: true},
		},
	})

	apitest.AssertGolden(t, "nutrition-overage.golden.json", MacroOverage{
		Message: "Monday would be 14 g over on carbs.", CanConfirm: true,
		Days: []DayOverageView{{
			DayID: uuid.MustParse("66666666-aaaa-aaaa-aaaa-666666666666"), Weekday: 1,
			Target:   MacrosView{Calories: 1331.2, ProteinG: 130, FatG: 70, CarbG: 40.3},
			Consumed: portion.Macros, Over: MacrosView{CarbG: 13.9},
		}},
	})

	apitest.AssertGolden(t, "nutrition-portions.golden.json", MealPortionList{Portions: []MealIngredientView{portion}})

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

// A slot's options project under its default, not as meals of their own, and
// the imported-line fields appear only where they are set.
func TestPlanDetailWithOptionsShape(t *testing.T) {
	t.Parallel()

	chicken := uuid.MustParse("11111111-bbbb-bbbb-bbbb-111111111111")
	fish := uuid.MustParse("22222222-bbbb-bbbb-bbbb-222222222222")
	plan := MealPlan{
		ID: uuid.MustParse("33333333-bbbb-bbbb-bbbb-333333333333"), Name: "Plano do nutricionista",
		Settings:    meal.PlanSettings{Type: meal.MidCarb, Mode: meal.Easy},
		Notes:       "Beber 2 L de água por dia.",
		TotalMacros: Macros{Calories: 165, ProteinG: 31, FatG: 3.6},
		Days: []meal.Day{{
			ID: uuid.MustParse("66666666-bbbb-bbbb-bbbb-666666666666"), Weekday: 1,
			Meals: []meal.Meal{{
				ID: uuid.MustParse("44444444-bbbb-bbbb-bbbb-444444444444"), MealNumber: 1, OptionIndex: 1,
				Name: "Almoço", OptionLabel: "Opção 1", TotalMacros: Macros{Calories: 165, ProteinG: 31, FatG: 3.6},
				Ingredients: []meal.MealIngredient{{
					ID: uuid.MustParse("55555555-bbbb-bbbb-bbbb-555555555555"), IngredientID: chicken,
					IngredientName: "Chicken breast", QuantityGrams: 100, Macros: Macros{Calories: 165, ProteinG: 31, FatG: 3.6},
				}},
				Alternatives: []meal.Meal{{
					ID: uuid.MustParse("44444444-cccc-cccc-cccc-444444444444"), MealNumber: 1, OptionIndex: 2,
					Name: "Almoço", OptionLabel: "Opção 2", TotalMacros: Macros{Calories: 206, ProteinG: 44, FatG: 2},
					Ingredients: []meal.MealIngredient{{
						ID: uuid.MustParse("55555555-cccc-cccc-cccc-555555555555"), IngredientID: fish,
						IngredientName: "Cod", QuantityGrams: 250, Macros: Macros{Calories: 206, ProteinG: 44, FatG: 2},
						SourceText: "peixe branco à vontade", Estimated: true,
					}},
				}},
			}},
		}},
	}
	apitest.AssertGolden(t, "nutrition-plan-with-options.golden.json", projectPlan(plan, nil))
}
