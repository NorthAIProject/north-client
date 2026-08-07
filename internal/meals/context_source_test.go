package meals_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestContextSourceIsFailSoftWithoutAGoal(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	repo := meals.NewRepository(pool)

	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)
	foodLogSvc := meals.NewFoodLogService(repo)
	progressSvc := meals.NewTrackMealProgressService(foodLogSvc, calculatorSvc)
	dietSvc := meals.NewDietPreferenceService(repo)

	source := meals.NewContextSource(progressSvc, dietSvc)

	into := &coach.Context{}
	err := source.Collect(context.Background(), coach.ContextRequest{User: user}, into)
	if err != nil {
		t.Fatalf("a missing goal should not fail the whole context build, got %v", err)
	}
	if len(into.Nutrition) != 0 {
		t.Fatalf("expected no nutrition section without a goal or diets, got %v", into.Nutrition)
	}
}

func TestContextSourceReportsProgressAndDiets(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	repo := meals.NewRepository(pool)
	ctx := context.Background()

	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	if _, err := biometricSvc.Record(ctx, user.ID, biometrics.Input{
		WeightKg: 80, HeightCm: 180, DateOfBirth: time.Now().AddDate(-30, 0, 0), Sex: biometrics.SexMale,
	}); err != nil {
		t.Fatalf("record biometrics: %v", err)
	}
	calculatorSvc := calculator.NewService(calculator.NewRepository(pool), biometricSvc)
	if _, err := calculatorSvc.Generate(ctx, user.ID, calculator.Input{Goal: calculator.GoalMaintenance}); err != nil {
		t.Fatalf("generate macro plan: %v", err)
	}

	dietSvc := meals.NewDietPreferenceService(repo)
	diets, err := dietSvc.ListDiets(ctx)
	if err != nil {
		t.Fatalf("list diets: %v", err)
	}
	if len(diets) == 0 {
		t.Fatal("expected the seeded diet reference list to be non-empty")
	}
	if err := dietSvc.SetUserDiets(ctx, user.ID, []uuid.UUID{diets[0].ID}); err != nil {
		t.Fatalf("set user diets: %v", err)
	}

	foodLogSvc := meals.NewFoodLogService(repo)
	progressSvc := meals.NewTrackMealProgressService(foodLogSvc, calculatorSvc)
	source := meals.NewContextSource(progressSvc, dietSvc)

	into := &coach.Context{}
	if err := source.Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Nutrition) != 2 {
		t.Fatalf("expected a progress line and a diet line, got %v", into.Nutrition)
	}
}
