package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/caffeine"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/fasting"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/soreness"
	"github.com/NorthAIProject/north-client/internal/supplements"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestPlanAndTrackerToolsWriteRealRows(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email: "plans@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "T", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}

	mealsRepo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(mealsRepo)
	planSvc := meals.NewMealPlanService(mealsRepo, fixedTarget{ProteinG: 150, FatG: 70, CarbG: 250})
	for _, in := range []meals.IngredientInput{
		{Name: "Zzoats test", ServingSizeGrams: 40, Per100g: meals.Macros{Calories: 380, ProteinG: 13, CarbG: 66, FatG: 7}},
		{Name: "Zzchicken test", ServingSizeGrams: 150, Per100g: meals.Macros{Calories: 165, ProteinG: 31, FatG: 4}},
	} {
		if _, err = ingredientSvc.Create(ctx, user.ID, in); err != nil {
			t.Fatalf("ingredient: %v", err)
		}
	}
	caffeineSvc := caffeine.NewService(caffeine.NewRepository(pool))
	supplementSvc := supplements.NewService(supplements.NewRepository(pool))
	fastingSvc := fasting.NewService(fasting.NewRepository(pool))
	sorenessSvc := soreness.NewService(soreness.NewRepository(pool))
	liftSvc := lifts.NewService(lifts.NewRepository(pool), nil)

	r := Build(Services{
		Users: userSvc, Ingredients: ingredientSvc, MealPlans: planSvc,
		Caffeine: caffeineSvc, Supplements: supplementSvc, Fasting: fastingSvc,
		Soreness: sorenessSvc, Lifts: liftSvc,
	})
	call := func(name string, args any) ai.ToolResult {
		t.Helper()
		raw, marshalErr := json.Marshal(args)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return r.Invoke(ctx, user.ID, ai.ToolCall{Name: name, Arguments: raw})
	}
	invoke := func(name string, args any) string {
		t.Helper()
		result := call(name, args)
		if result.IsError {
			t.Fatalf("%s: %s", name, result.Content)
		}
		return result.Content
	}

	out := invoke("create_meal_plan", map[string]any{
		"name": "Cut", "objective": "cutting",
		"days": []map[string]any{{"meals": []map[string]any{
			{"name": "Breakfast", "ingredients": []map[string]any{{"food": "zzoats", "grams": 80}}},
			{"name": "Lunch", "ingredients": []map[string]any{{"food": "zzchicken", "grams": 200}}},
		}}},
	})
	if !strings.Contains(out, "Breakfast") || !strings.Contains(out, "Lunch") {
		t.Errorf("create_meal_plan said %q", out)
	}
	plans, err := planSvc.ListPlans(ctx, user.ID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans = %d, %v", len(plans), err)
	}

	// An ingredient that does not resolve stops the plan before anything is
	// written.
	if res := call("create_meal_plan", map[string]any{
		"name": "Broken", "days": []map[string]any{{"meals": []map[string]any{
			{"name": "Dinner", "ingredients": []map[string]any{{"food": "unobtainium", "grams": 100}}},
		}}},
	}); !res.IsError {
		t.Errorf("an unknown ingredient was accepted: %s", res.Content)
	}

	// A day over the target is refused with the amount, and nothing is saved:
	// 300 g oats is 198 g carbs against mid carb's 88.75 g.
	if res := call("create_meal_plan", map[string]any{
		"name": "Too much", "plan_type": "mid_carb", "days": []map[string]any{{"meals": []map[string]any{
			{"name": "Breakfast", "ingredients": []map[string]any{{"food": "zzoats", "grams": 300}}},
		}}},
	}); !res.IsError || !strings.Contains(res.Content, "over on carbs") {
		t.Errorf("an over-target plan was not refused with the amount: %s", res.Content)
	}
	if plans, _ = planSvc.ListPlans(ctx, user.ID); len(plans) != 1 {
		t.Errorf("failed plans left %d plans", len(plans))
	}

	invoke("log_caffeine", map[string]any{"preset": "espresso"})
	if drinks, _ := caffeineSvc.Today(ctx, user); len(drinks) != 1 || drinks[0].MG != 63 {
		t.Errorf("caffeine = %+v", drinks)
	}

	invoke("log_supplement", map[string]any{"preset": "vitamin_d3"})
	if taken, _ := supplementSvc.Today(ctx, user); len(taken) != 1 {
		t.Errorf("supplements = %+v", taken)
	}

	invoke("start_fast", map[string]any{"target_hours": 14})
	if _, open, _ := fastingSvc.Current(ctx, user); !open {
		t.Error("no fast open after start_fast")
	}
	invoke("stop_fast", map[string]any{})

	invoke("record_soreness", map[string]any{"region": "quads", "severity": 2})

	out = invoke("log_lift_set", map[string]any{"exercise": "Back squat", "weight_kg": 100, "reps": 5, "sets": 3})
	if !strings.Contains(out, "3 set") {
		t.Errorf("log_lift_set said %q", out)
	}
	invoke("log_lift_set", map[string]any{"exercise": "back squat", "weight_kg": 105, "reps": 3})
	last, err := liftSvc.Last(ctx, user, []string{"Back squat"})
	if err != nil {
		t.Fatal(err)
	}
	if sets := last["back squat"]; len(sets) != 4 || sets[3].SetNumber != 4 {
		t.Errorf("squat sets = %+v, want four numbered 1-4", sets)
	}
	if out = invoke("get_lift_stats", map[string]any{"range": "week"}); !strings.Contains(out, "4 sets") {
		t.Errorf("get_lift_stats said %q", out)
	}
}

func TestClockTodayReadsAFutureTimeAsYesterday(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	now := time.Date(2026, 9, 26, 7, 0, 0, 0, loc)
	at, err := clockToday("20:00", loc, now)
	if err != nil || at.Day() != 25 {
		t.Errorf("20:00 at 07:00 = %v, %v; want yesterday", at, err)
	}
	if at, _ = clockToday("06:30", loc, now); at.Day() != 26 {
		t.Errorf("06:30 = %v, want today", at)
	}
	if _, err = clockToday("8pm", loc, now); err == nil {
		t.Error("8pm accepted")
	}
}

// fixedTarget is a macro target from the calculator that never changes.
type fixedTarget calculator.MacroPlan

func (f fixedTarget) Current(context.Context, uuid.UUID) (calculator.MacroPlan, error) {
	return calculator.MacroPlan(f), nil
}
