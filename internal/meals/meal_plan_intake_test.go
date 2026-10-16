package meals_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestValidateMealPlanIntake(t *testing.T) {
	t.Parallel()

	good := meals.MealPlanIntake{
		Objective: " cutting ", Days: 3, MealsPerDay: 4, PlanType: meal.LowCarb,
		Exclusions: []string{" peanuts", "", "Peanuts", "dairy"}, Notes: "  cheap  ",
	}
	clean, err := meals.ValidateMealPlanIntake(good)
	if err != nil {
		t.Fatalf("valid intake refused: %v", err)
	}
	if clean.Objective != "cutting" || clean.Notes != "cheap" || strings.Join(clean.Exclusions, "|") != "peanuts|dairy" {
		t.Fatalf("clean = %+v", clean)
	}

	_, err = meals.ValidateMealPlanIntake(meals.MealPlanIntake{
		Objective: "shredding", Days: 8, MealsPerDay: 0, PlanType: meal.Custom,
		Exclusions: []string{strings.Repeat("x", 61)}, Notes: strings.Repeat("y", 501),
	})
	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) {
		t.Fatalf("err = %v", err)
	}
	got := fields.Messages()
	for _, field := range []string{"objective", "days", "meals_per_day", "plan_type", "exclusions", "notes"} {
		if got[field] == "" {
			t.Errorf("no error for %s: %v", field, got)
		}
	}
}

func TestParseExclusionsSplitsWhatPeopleType(t *testing.T) {
	t.Parallel()

	got := meals.ParseExclusions("dairy, peanuts;mushrooms\nDairy,,  ")
	if strings.Join(got, "|") != "dairy|peanuts|mushrooms" {
		t.Fatalf("got %q", got)
	}
}

// What someone won't eat reaches the coach in every conversation, even
// before they have a macro target.
func TestContextSourceReportsFoodsTheyWontEat(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	repo := meals.NewRepository(pool)
	ctx := context.Background()

	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)
	progressSvc := meals.NewTrackMealProgressService(meals.NewFoodLogService(repo), calculatorSvc)
	source := meals.NewContextSource(progressSvc, meals.NewDietPreferenceService(repo))

	plans := meals.NewMealPlanService(repo, calculatorSvc)
	for _, exclusions := range [][]string{{"pork"}, {"dairy", "peanuts"}} {
		if _, err := plans.SaveIntake(ctx, user.ID, meals.MealPlanIntake{
			Objective: "cutting", Days: 1, MealsPerDay: 3, PlanType: meal.MidCarb, Exclusions: exclusions,
		}); err != nil {
			t.Fatalf("save intake: %v", err)
		}
	}

	into := &coach.Context{}
	if err := source.Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Nutrition) != 1 || into.Nutrition[0] != "Won't eat: dairy, peanuts" {
		t.Fatalf("nutrition = %q, want only the latest answers", into.Nutrition)
	}
}
