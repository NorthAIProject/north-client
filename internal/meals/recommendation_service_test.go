package meals_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
)

func TestRecommendationRuleBranches(t *testing.T) {
	tests := []struct {
		name        string
		goal        string
		skipLogging bool
		deltaPerDay float64 // ignored when skipLogging
		wantReason  string
	}{
		{name: "no logging reads as inconsistent", goal: calculator.GoalMaintenance, skipLogging: true, wantReason: "low adherence"},
		{name: "cutting on track stays on track", goal: calculator.GoalCutting, deltaPerDay: 0, wantReason: "within tolerance"},
		{name: "cutting eating over deficit gets flagged", goal: calculator.GoalCutting, deltaPerDay: 300, wantReason: "trailing average over cutting goal"},
		{name: "bulking on track stays on track", goal: calculator.GoalBulking, deltaPerDay: 0, wantReason: "within tolerance"},
		{name: "bulking under-eating gets flagged", goal: calculator.GoalBulking, deltaPerDay: -300, wantReason: "trailing average under bulking goal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, user, foodLogSvc, _, recommendSvc, calculatorSvc, fillerID := setupWithGoal(t, tt.goal)
			ctx := context.Background()

			goalPlan, err := calculatorSvc.Current(ctx, user.ID)
			if err != nil {
				t.Fatalf("current macro plan: %v", err)
			}

			if !tt.skipLogging {
				kcal := goalPlan.CalorieGoal + tt.deltaPerDay
				for d := 0; d < 14; d++ {
					day := time.Now().AddDate(0, 0, -d)
					if _, err := foodLogSvc.LogIngredient(ctx, user.ID, meals.LogIngredientInput{
						IngredientID: fillerID, QuantityGrams: kcal, LogDate: day,
					}); err != nil {
						t.Fatalf("log day %d: %v", d, err)
					}
				}
			}

			rec, err := recommendSvc.Recommend(ctx, user.ID)
			if err != nil {
				t.Fatalf("recommend: %v", err)
			}
			if !strings.Contains(rec.Reason, tt.wantReason) {
				t.Fatalf("reason = %q, want to contain %q (message: %q)", rec.Reason, tt.wantReason, rec.Message)
			}
		})
	}
}
