package calculator

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/calculator/macroplan"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestCalculatorShapes(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "calculator.golden.json", CalculatorView{
		Biometrics: &BiometricsView{WeightKg: 72, HeightCm: 175, DateOfBirth: "1990-05-01", Sex: "female", AgeYears: 36},
		Goal: &MacroGoalView{
			ActivityLevel: "moderate", Goal: "maintenance", MacroSplit: "moderate_carb", BMR: 1467, TDEE: 2274,
			CalorieGoal: 2274, ProteinG: 142, FatG: 76, CarbG: 256, CreatedAt: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
			Comparison: []GoalCalories{{Goal: "cutting", CalorieGoal: 1824}, {Goal: "maintenance", CalorieGoal: 2274}, {Goal: "bulking", CalorieGoal: 2624}},
		},
		Options: CalculatorOptions{ActivityLevels: macroplan.ActivityLevels, Goals: macroplan.Goals, MacroSplits: macroplan.Splits},
	})
}
