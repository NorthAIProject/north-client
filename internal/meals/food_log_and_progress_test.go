package meals_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestFoodLogDayAndDailyTotals(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	foodLogSvc := meals.NewFoodLogService(repo)
	ctx := context.Background()

	oats, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "Oats", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 389, ProteinG: 16.9, FatG: 6.9, CarbG: 66.3},
	})
	if err != nil {
		t.Fatalf("create oats: %v", err)
	}

	today := time.Now()
	if _, err = foodLogSvc.LogIngredient(ctx, user.ID, meals.LogIngredientInput{IngredientID: oats.ID, QuantityGrams: 50, LogDate: today}); err != nil {
		t.Fatalf("log ingredient: %v", err)
	}

	entries, err := foodLogSvc.Day(ctx, user.ID, today)
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "Oats" {
		t.Fatalf("label = %q", entries[0].Label)
	}

	totals, err := foodLogSvc.DailyTotals(ctx, user.ID, today)
	if err != nil {
		t.Fatalf("daily totals: %v", err)
	}
	// 50g of 389kcal/100g = 194.5 kcal.
	if !within(totals.Calories, 194.5, 0.01) {
		t.Fatalf("daily total calories = %v, want 194.5", totals.Calories)
	}
}

// setupWithGoal wires biometrics + calculator + meals against one pool/user
// and generates a macro plan for the given goal, returning everything a
// progress/recommendation test needs.
func setupWithGoal(t *testing.T, goal string) (pool *pgxpool.Pool, user users.User, foodLogSvc *meals.FoodLogService, progressSvc *meals.TrackMealProgressService, recommendSvc *meals.GoalRecommendationService, calculatorSvc *calculator.Service, fillerIngredientID uuid.UUID) {
	t.Helper()

	pool = testdb.New(t)
	user = newUser(t, pool, "fernando@north.test")
	ctx := context.Background()

	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	if _, err := biometricSvc.Record(ctx, user.ID, biometrics.Input{
		WeightKg: 80, HeightCm: 180, DateOfBirth: time.Now().AddDate(-30, 0, 0), Sex: biometrics.SexMale,
	}); err != nil {
		t.Fatalf("record biometrics: %v", err)
	}

	calculatorSvc = calculator.NewService(calculator.NewRepository(pool), biometricSvc)
	if _, err := calculatorSvc.Generate(ctx, user.ID, calculator.Input{
		ActivityLevel: calculator.ActivitySedentary, Goal: goal, MacroSplit: calculator.SplitModerateCarb,
	}); err != nil {
		t.Fatalf("generate macro plan: %v", err)
	}

	mealsRepo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(mealsRepo)
	foodLogSvc = meals.NewFoodLogService(mealsRepo)
	progressSvc = meals.NewTrackMealProgressService(foodLogSvc, calculatorSvc)
	recommendSvc = meals.NewGoalRecommendationService(progressSvc, calculatorSvc)

	// 1 kcal per gram, so logging N grams logs exactly N kcal — makes the
	// arithmetic in each test readable.
	oneKcalPerGram, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "Test filler", Category: meals.CategoryOther,
		Per100g: meals.Macros{Calories: 100},
	})
	if err != nil {
		t.Fatalf("create filler ingredient: %v", err)
	}

	return pool, user, foodLogSvc, progressSvc, recommendSvc, calculatorSvc, oneKcalPerGram.ID
}

// TestProgressForDayComputesDelta verifies the delta math against a real
// generated goal: logging exactly the goal's calories should show a zero
// delta, and logging more should show a positive one.
func TestProgressForDayComputesDelta(t *testing.T) {
	_, user, foodLogSvc, progressSvc, _, calculatorSvc, fillerID := setupWithGoal(t, calculator.GoalMaintenance)
	ctx := context.Background()

	goalPlan, err := calculatorSvc.Current(ctx, user.ID)
	if err != nil {
		t.Fatalf("current macro plan: %v", err)
	}

	today := time.Now()
	// One filler gram = one kcal, so this logs exactly the goal's calories.
	if _, err = foodLogSvc.LogIngredient(ctx, user.ID, meals.LogIngredientInput{
		IngredientID: fillerID, QuantityGrams: goalPlan.CalorieGoal, LogDate: today,
	}); err != nil {
		t.Fatalf("log ingredient: %v", err)
	}

	progress, err := progressSvc.ForDay(ctx, user.ID, today)
	if err != nil {
		t.Fatalf("for day: %v", err)
	}
	if !within(progress.DeltaCalories, 0, 0.5) {
		t.Fatalf("delta calories = %v, want ~0 (logged exactly the goal)", progress.DeltaCalories)
	}
	if progress.OverCalories() {
		t.Fatal("logging exactly the goal should not read as over")
	}
}

func TestProgressForDayWithoutGoalIsNotFound(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "fernando@north.test")
	repo := meals.NewRepository(pool)
	foodLogSvc := meals.NewFoodLogService(repo)

	// A calculator whose Current always reports not found, standing in for
	// a user who has not generated a plan yet.
	progressSvc := meals.NewTrackMealProgressService(foodLogSvc, notFoundGoalLookup{})

	if _, err := progressSvc.ForDay(context.Background(), user.ID, time.Now()); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

type notFoundGoalLookup struct{}

func (notFoundGoalLookup) Current(context.Context, uuid.UUID) (calculator.MacroPlan, error) {
	return calculator.MacroPlan{}, apperr.ErrNotFound
}
