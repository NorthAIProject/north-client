package meals_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func within(got, want, tolerance float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff < tolerance
}

// goalStub is the calculator as the plan service sees it. A nil plan is a
// person who has not worked out a target yet.
type goalStub struct {
	mu   sync.Mutex
	plan *calculator.MacroPlan
}

func (g *goalStub) Current(context.Context, uuid.UUID) (calculator.MacroPlan, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.plan == nil {
		return calculator.MacroPlan{}, apperr.ErrNotFound
	}
	return *g.plan, nil
}

func (g *goalStub) clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.plan = nil
}

// planFixture is a person with a 150 g protein / 70 g fat / 250 g carb target
// and two foods: chicken (31 g protein per 100 g) and rice (28 g carbs per
// 100 g).
type planFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	svc     *meals.MealPlanService
	goals   *goalStub
	userID  uuid.UUID
	chicken meals.Ingredient
	rice    meals.Ingredient
}

func newPlanFixture(t *testing.T, email string) planFixture {
	t.Helper()
	pool := testdb.New(t)
	user := newUser(t, pool, email)
	repo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(repo)
	goals := &goalStub{plan: &calculator.MacroPlan{CalorieGoal: 2230, ProteinG: 150, FatG: 70, CarbG: 250}}
	ctx := context.Background()

	chicken, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "Chicken breast", Category: meals.CategoryProtein,
		Per100g: meals.Macros{Calories: 165, ProteinG: 31, FatG: 3.6, CarbG: 0},
	})
	if err != nil {
		t.Fatalf("create chicken: %v", err)
	}
	rice, err := ingredientSvc.Create(ctx, user.ID, meals.IngredientInput{
		Name: "White rice", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28},
	})
	if err != nil {
		t.Fatalf("create rice: %v", err)
	}
	return planFixture{
		ctx: ctx, pool: pool, svc: meals.NewMealPlanService(repo, goals), goals: goals, userID: user.ID,
		chicken: chicken, rice: rice,
	}
}

func (f planFixture) plan(t *testing.T, s meal.PlanSettings, weekdays ...time.Weekday) meals.MealPlan {
	t.Helper()
	in := meals.MealPlanInput{Name: "Plan", Settings: s, Weekdays: weekdays}
	if len(weekdays) == 0 {
		in.DayCount = 1
	}
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, in, nil, false)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan
}

func (f planFixture) meal(t *testing.T, day meal.Day, name string) meals.Meal {
	t.Helper()
	m, err := f.svc.AddMeal(f.ctx, day.ID, f.userID, name)
	if err != nil {
		t.Fatalf("add meal: %v", err)
	}
	return m
}

func (f planFixture) add(m meals.Meal, in meals.Ingredient, grams float64, confirm bool) error {
	_, err := f.svc.AddIngredient(f.ctx, m.ID, f.userID, meals.MealIngredientInput{IngredientID: in.ID, QuantityGrams: grams}, confirm)
	return err
}

func (f planFixture) reload(t *testing.T, id uuid.UUID) meals.MealPlan {
	t.Helper()
	plan, err := f.svc.GetPlan(f.ctx, id, f.userID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	return plan
}

var (
	easyLow     = meal.PlanSettings{Type: meal.LowCarb, Mode: meal.Easy}
	easyMid     = meal.PlanSettings{Type: meal.MidCarb, Mode: meal.Easy}
	advancedMid = meal.PlanSettings{Type: meal.MidCarb, Mode: meal.Advanced}
)

// overage asserts err refused a change for going over, and returns the verdict.
func overage(t *testing.T, err error) meal.Verdict {
	t.Helper()
	var over *meals.OverageError
	if !errors.As(err, &over) {
		t.Fatalf("expected an overage, got %v", err)
	}
	return over.Verdict
}

func fieldError(t *testing.T, err error, field string) {
	t.Helper()
	var fields apperr.FieldErrors
	if !errors.As(err, &fields) || fields.Messages()[field] == "" {
		t.Fatalf("expected a %s field error, got %v", field, err)
	}
}

// TestTotalsAlwaysEqualTheSumOfChildren is the most important test in this
// package: meal and plan total_macros are a cache, and a cache that drifts
// from its source is worse than no cache at all.
func TestTotalsAlwaysEqualTheSumOfChildren(t *testing.T) {
	f := newPlanFixture(t, "totals@north.test")
	plan := f.plan(t, easyMid)
	lunch := f.meal(t, plan.Days[0], "Lunch")

	chickenLine, err := f.svc.AddIngredient(f.ctx, lunch.ID, f.userID, meals.MealIngredientInput{IngredientID: f.chicken.ID, QuantityGrams: 200}, false)
	if err != nil {
		t.Fatalf("add chicken: %v", err)
	}
	if err := f.add(lunch, f.rice, 150, false); err != nil {
		t.Fatalf("add rice: %v", err)
	}

	// 200g chicken (330 kcal) + 150g rice (195 kcal) = 525 kcal.
	loaded := f.reload(t, plan.ID)
	day := loaded.Days[0]
	if len(day.Meals) != 1 || len(day.Meals[0].Ingredients) != 2 {
		t.Fatalf("expected 1 meal with 2 ingredients, got %+v", day.Meals)
	}
	if !within(day.Meals[0].TotalMacros.Calories, 525, 0.01) || !within(loaded.TotalMacros.Calories, 525, 0.01) {
		t.Fatalf("totals = meal %v, plan %v; want 525", day.Meals[0].TotalMacros.Calories, loaded.TotalMacros.Calories)
	}

	if err := f.svc.RemoveIngredient(f.ctx, chickenLine.ID, f.userID); err != nil {
		t.Fatalf("remove chicken: %v", err)
	}
	if got := f.reload(t, plan.ID).TotalMacros.Calories; !within(got, 195, 0.01) {
		t.Fatalf("plan total after removal = %v, want 195 (rice only)", got)
	}

	if err := f.svc.RemoveMeal(f.ctx, lunch.ID, f.userID); err != nil {
		t.Fatalf("remove meal: %v", err)
	}
	if got := f.reload(t, plan.ID); len(got.Days[0].Meals) != 0 || got.TotalMacros.Calories != 0 {
		t.Fatalf("after removing its only meal: %d meals, %v kcal", len(got.Days[0].Meals), got.TotalMacros.Calories)
	}
}

func TestPlansAreScopedToTheirOwner(t *testing.T) {
	f := newPlanFixture(t, "owner@north.test")
	stranger := newUser(t, f.pool, "stranger@north.test")
	plan := f.plan(t, advancedMid)

	if _, err := f.svc.GetPlan(f.ctx, plan.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("get: expected ErrNotFound, got %v", err)
	}
	if _, err := f.svc.AddMeal(f.ctx, plan.Days[0].ID, stranger.ID, "Hijack"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("add meal: expected ErrNotFound, got %v", err)
	}
	if err := f.svc.UpdateDay(f.ctx, plan.Days[0].ID, stranger.ID, meal.DayOverride{}, false); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("update day: expected ErrNotFound, got %v", err)
	}
}

func TestValidationRejectsMissingPlanName(t *testing.T) {
	t.Parallel()

	_, err := meals.ValidateMealPlan(meals.MealPlanInput{Settings: easyMid, DayCount: 1})
	fieldError(t, err, "name")
}

func TestValidationKeepsCustomCarbsToAdvanced(t *testing.T) {
	t.Parallel()

	pct := 40.0
	_, err := meals.ValidateMealPlan(meals.MealPlanInput{
		Name: "Plan", DayCount: 1, Settings: meal.PlanSettings{Type: meal.Custom, CustomCarbPct: &pct, Mode: meal.Easy},
	})
	fieldError(t, err, "plan_type")

	_, err = meals.ValidateMealPlan(meals.MealPlanInput{
		Name: "Plan", Weekdays: []time.Weekday{time.Monday}, Settings: easyMid,
	})
	fieldError(t, err, "weekdays")
}

func TestValidationCapsPlanNotes(t *testing.T) {
	t.Parallel()

	_, err := meals.ValidateMealPlan(meals.MealPlanInput{
		Name: "Plan", DayCount: 1, Settings: easyMid, Notes: strings.Repeat("á", meals.MaxPlanNotesRunes+1),
	})
	fieldError(t, err, "notes")

	in, err := meals.ValidateMealPlan(meals.MealPlanInput{
		Name: "Plan", DayCount: 1, Settings: easyMid, Notes: " " + strings.Repeat("á", meals.MaxPlanNotesRunes) + " ",
	})
	if err != nil || utf8.RuneCountInString(in.Notes) != meals.MaxPlanNotesRunes {
		t.Fatalf("notes at the cap: err = %v, %d runes", err, utf8.RuneCountInString(in.Notes))
	}
}

// With no target there is nothing to hold a plan to, so nothing is let through
// unchecked: no new plan, and no food on an existing one.
func TestNoMacroTargetRefusesPlansAndFood(t *testing.T) {
	f := newPlanFixture(t, "notarget@north.test")
	plan := f.plan(t, easyMid)
	lunch := f.meal(t, plan.Days[0], "Lunch")
	f.goals.clear()

	_, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Another", Settings: easyMid, DayCount: 1}, nil, false)
	fieldError(t, err, "macro_target")
	fieldError(t, f.add(lunch, f.rice, 10, true), "macro_target")

	if n := len(f.reload(t, plan.ID).Days[0].Meals[0].Ingredients); n != 0 {
		t.Fatalf("food was added without a target: %d lines", n)
	}
}

func TestEasyPlanFillsDaysFromMondayAtTheMidpoint(t *testing.T) {
	f := newPlanFixture(t, "easy@north.test")
	plan, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Week", Settings: easyLow, DayCount: 5}, nil, false)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	want := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	if got := plan.Weekdays(); len(got) != len(want) {
		t.Fatalf("weekdays = %v, want %v", got, want)
	}
	target, err := f.svc.ActiveTarget(f.ctx, f.userID)
	if err != nil || target == nil {
		t.Fatalf("active target: %v, %v", target, err)
	}
	for i, st := range plan.State().Statuses(*target) {
		if plan.Days[i].Weekday != want[i] {
			t.Errorf("day %d is %s, want %s", i, plan.Days[i].Weekday, want[i])
		}
		// Low carb is 6–25% of 250 g; the midpoint, 15.5%, is 38.75 g.
		if !within(st.Target.CarbG, 38.75, 0.001) || st.Target.ProteinG != 150 || st.Target.FatG != 70 {
			t.Errorf("%s target = %+v, want 38.75 g carbs at the active protein and fat", plan.Days[i].Weekday, st.Target)
		}
	}
}

func TestEasyOverageIsRefusedEvenWhenConfirmed(t *testing.T) {
	f := newPlanFixture(t, "easyover@north.test")
	plan := f.plan(t, easyLow)
	lunch := f.meal(t, plan.Days[0], "Lunch")

	// 200 g rice is 56 g carbs against a 38.75 g day.
	for _, confirm := range []bool{false, true} {
		over := overage(t, f.add(lunch, f.rice, 200, confirm))
		if over.CanConfirm {
			t.Fatal("an easy plan offered to confirm an overage")
		}
		if got := over.Over[0].Status.Over.CarbG; !within(got, 17.25, 0.001) {
			t.Fatalf("carbs over = %v, want 17.25", got)
		}
	}
	if loaded := f.reload(t, plan.ID); len(loaded.Days[0].Meals[0].Ingredients) != 0 || loaded.TotalMacros.Calories != 0 {
		t.Fatalf("a refused portion was written: %+v", loaded.Days[0].Meals[0])
	}
}

func TestAdvancedOverageNeedsConfirmation(t *testing.T) {
	f := newPlanFixture(t, "advancedover@north.test")
	plan := f.plan(t, meal.PlanSettings{Type: meal.LowCarb, Mode: meal.Advanced})
	lunch := f.meal(t, plan.Days[0], "Lunch")

	if over := overage(t, f.add(lunch, f.rice, 200, false)); !over.CanConfirm {
		t.Fatal("an advanced plan did not offer to confirm")
	}
	if err := f.add(lunch, f.rice, 200, true); err != nil {
		t.Fatalf("confirmed overage refused: %v", err)
	}
	// Protein has room, and carbs do not grow: no question to ask.
	if err := f.add(lunch, f.chicken, 100, false); err != nil {
		t.Fatalf("chicken on a carb-over day: %v", err)
	}
	// More carbs grow the overage, so it is asked again.
	overage(t, f.add(lunch, f.rice, 20, false))
}

// The acceptance case: Monday low carb and Saturday high carb in one plan.
func TestAdvancedDaysCanEachHaveTheirOwnTarget(t *testing.T) {
	f := newPlanFixture(t, "days@north.test")
	plan := f.plan(t, advancedMid, time.Saturday, time.Monday)
	low, high := meal.LowCarb, meal.HighCarb
	monday, saturday := plan.Days[0], plan.Days[1]
	if monday.Weekday != time.Monday || saturday.Weekday != time.Saturday {
		t.Fatalf("days run %v, want Monday first", plan.Weekdays())
	}

	if err := f.svc.UpdateDay(f.ctx, monday.ID, f.userID, meal.DayOverride{CarbType: &low}, false); err != nil {
		t.Fatalf("monday low carb: %v", err)
	}
	if err := f.svc.UpdateDay(f.ctx, saturday.ID, f.userID, meal.DayOverride{CarbType: &high}, false); err != nil {
		t.Fatalf("saturday high carb: %v", err)
	}

	loaded := f.reload(t, plan.ID)
	target, _ := f.svc.ActiveTarget(f.ctx, f.userID)
	st := loaded.State().Statuses(*target)
	if !within(st[0].Target.CarbG, 38.75, 0.001) || !within(st[1].Target.CarbG, 138.75, 0.001) {
		t.Fatalf("carb targets = %v and %v, want 38.75 and 138.75", st[0].Target.CarbG, st[1].Target.CarbG)
	}
}

func TestEasyPlansHaveNoDayOverrides(t *testing.T) {
	f := newPlanFixture(t, "easyday@north.test")
	plan := f.plan(t, easyMid)
	low := meal.LowCarb
	fieldError(t, f.svc.UpdateDay(f.ctx, plan.Days[0].ID, f.userID, meal.DayOverride{CarbType: &low}, false), "mode")
}

func TestOverridesCannotExceedTheActiveTarget(t *testing.T) {
	f := newPlanFixture(t, "ceiling@north.test")
	plan := f.plan(t, advancedMid)
	carbs, protein := 300.0, 151.0
	err := f.svc.UpdateDay(f.ctx, plan.Days[0].ID, f.userID, meal.DayOverride{CarbG: &carbs, ProteinG: &protein}, false)
	fieldError(t, err, "carb_g")
	fieldError(t, err, "protein_g")
}

func TestLoweringThePlanTypeIsChecked(t *testing.T) {
	f := newPlanFixture(t, "lower@north.test")
	plan := f.plan(t, easyMid)
	lunch := f.meal(t, plan.Days[0], "Lunch")
	// 250 g rice is 70 g carbs: inside mid carb's 88.75 g, past low carb's 38.75 g.
	if err := f.add(lunch, f.rice, 250, false); err != nil {
		t.Fatalf("add rice: %v", err)
	}

	err := f.svc.UpdateSettings(f.ctx, plan.ID, f.userID, meals.PlanSettingsInput{Name: "Plan", Settings: easyLow})
	overage(t, err)
	if got := f.reload(t, plan.ID).Settings.Type; got != meal.MidCarb {
		t.Fatalf("plan type = %s after a refused change", got)
	}
}

func TestSwitchingToEasyNeedsConfirmationAndResetsDays(t *testing.T) {
	f := newPlanFixture(t, "toeasy@north.test")
	plan := f.plan(t, advancedMid)
	low := meal.LowCarb
	if err := f.svc.UpdateDay(f.ctx, plan.Days[0].ID, f.userID, meal.DayOverride{CarbType: &low}, false); err != nil {
		t.Fatalf("override: %v", err)
	}

	toEasy := meals.PlanSettingsInput{Name: "Plan", Settings: easyMid}
	fieldError(t, f.svc.UpdateSettings(f.ctx, plan.ID, f.userID, toEasy), "confirm_reset")

	toEasy.ConfirmReset = true
	if err := f.svc.UpdateSettings(f.ctx, plan.ID, f.userID, toEasy); err != nil {
		t.Fatalf("switch to easy: %v", err)
	}
	loaded := f.reload(t, plan.ID)
	if loaded.Settings.Mode != meal.Easy || !loaded.Days[0].Override.IsZero() {
		t.Fatalf("after switching: mode %s, override %+v", loaded.Settings.Mode, loaded.Days[0].Override)
	}
}

func TestSwitchingToEasyIsRefusedWhileADayIsOver(t *testing.T) {
	f := newPlanFixture(t, "toeasyover@north.test")
	plan := f.plan(t, advancedMid)
	lunch := f.meal(t, plan.Days[0], "Lunch")
	if err := f.add(lunch, f.rice, 400, true); err != nil {
		t.Fatalf("confirmed overage: %v", err)
	}

	err := f.svc.UpdateSettings(f.ctx, plan.ID, f.userID, meals.PlanSettingsInput{Name: "Plan", Settings: easyMid, ConfirmReset: true})
	if over := overage(t, err); over.CanConfirm {
		t.Fatal("switching to easy offered to confirm an overage")
	}
}

func TestEasyStaysWithinTheTargetWhenTheyAddDays(t *testing.T) {
	f := newPlanFixture(t, "adddays@north.test")
	plan := f.plan(t, easyMid)

	day, err := f.svc.AddDay(f.ctx, plan.ID, f.userID, nil)
	if err != nil || day.Weekday != time.Tuesday {
		t.Fatalf("add day = %v, %v; want Tuesday", day.Weekday, err)
	}
	sunday := time.Sunday
	_, err = f.svc.AddDay(f.ctx, plan.ID, f.userID, &sunday)
	fieldError(t, err, "weekday")

	if err := f.svc.RemoveDay(f.ctx, day.ID, f.userID); err != nil {
		t.Fatalf("remove day: %v", err)
	}
	fieldError(t, f.svc.RemoveDay(f.ctx, plan.Days[0].ID, f.userID), "day")
}

func TestMealsAreNumberedWithinTheirDay(t *testing.T) {
	f := newPlanFixture(t, "numbers@north.test")
	plan := f.plan(t, advancedMid, time.Monday, time.Tuesday)

	got := []int{
		f.meal(t, plan.Days[0], "Breakfast").MealNumber,
		f.meal(t, plan.Days[0], "Lunch").MealNumber,
		f.meal(t, plan.Days[1], "Breakfast").MealNumber,
	}
	if got[0] != 1 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("meal numbers = %v, want [1 2 1]", got)
	}
}

// Two portions that each fit but together go over cannot both pass the check:
// the plan's row lock makes the second see the first.
func TestConcurrentAddsCannotTogetherGoOver(t *testing.T) {
	f := newPlanFixture(t, "race@north.test")
	plan := f.plan(t, easyLow)
	lunch := f.meal(t, plan.Days[0], "Lunch")

	// 100 g rice is 28 g carbs; two are 56 g against a 38.75 g day.
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = f.add(lunch, f.rice, 100, false)
		}()
	}
	wg.Wait()

	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("want exactly one add to succeed, got %v and %v", errs[0], errs[1])
	}
}

// A spoken meal lands as one batch. Every line reaches the meal and the totals
// still equal the sum of their children — the cache the test above guards.
func TestAddIngredientsAddsEveryLine(t *testing.T) {
	f := newPlanFixture(t, "batch@north.test")
	plan := f.plan(t, easyMid)
	lunch := f.meal(t, plan.Days[0], "Lunch")

	added, err := f.svc.AddIngredients(f.ctx, lunch.ID, f.userID, []meals.MealIngredientInput{
		{IngredientID: f.chicken.ID, QuantityGrams: 200},
		{IngredientID: f.rice.ID, QuantityGrams: 150},
	}, false)
	if err != nil {
		t.Fatalf("add ingredients: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("added %d lines, want 2", len(added))
	}
	if got := f.reload(t, plan.ID).Days[0].Meals[0].TotalMacros.Calories; !within(got, 525, 0.01) {
		t.Fatalf("meal total calories = %v, want 525", got)
	}
}

// One bad line, or a batch that together goes over, refuses the batch before
// anything is written: half a spoken meal on the plan, with no way to tell
// which half, is worse than none.
func TestAddIngredientsWritesNothingWhenRefused(t *testing.T) {
	f := newPlanFixture(t, "badbatch@north.test")
	stranger := newUser(t, f.pool, "batchstranger@north.test")
	plan := f.plan(t, easyLow)
	lunch := f.meal(t, plan.Days[0], "Lunch")

	strangerSvc := meals.NewIngredientService(meals.NewRepository(f.pool))
	private, err := strangerSvc.Create(f.ctx, stranger.ID, meals.IngredientInput{
		Name: "Stranger's granola", Category: meals.CategoryCarb, Per100g: meals.Macros{Calories: 450},
	})
	if err != nil {
		t.Fatalf("create stranger's ingredient: %v", err)
	}

	for name, lines := range map[string][]meals.MealIngredientInput{
		"zero grams":                {{IngredientID: f.chicken.ID, QuantityGrams: 200}, {IngredientID: f.chicken.ID, QuantityGrams: 0}},
		"another account's private": {{IngredientID: f.chicken.ID, QuantityGrams: 200}, {IngredientID: private.ID, QuantityGrams: 50}},
		"over together":             {{IngredientID: f.rice.ID, QuantityGrams: 100}, {IngredientID: f.rice.ID, QuantityGrams: 100}},
		"empty":                     nil,
	} {
		if _, refused := f.svc.AddIngredients(f.ctx, lunch.ID, f.userID, lines, false); refused == nil {
			t.Errorf("%s: batch was accepted", name)
		}
	}

	if n := len(f.reload(t, plan.ID).Days[0].Meals[0].Ingredients); n != 0 {
		t.Fatalf("a refused batch left %d lines on the meal", n)
	}
}

func TestCreatingAPlanThatStartsOverWritesNothing(t *testing.T) {
	f := newPlanFixture(t, "drafts@north.test")
	days := []meals.DayDraft{{Meals: []meals.MealDraft{{
		Name: "Lunch", Portions: []meals.MealIngredientInput{{IngredientID: f.rice.ID, QuantityGrams: 300}},
	}}}}

	_, err := f.svc.CreatePlan(f.ctx, f.userID, meals.MealPlanInput{Name: "Too much", Settings: easyLow, DayCount: 1}, days, false)
	overage(t, err)
	plans, err := f.svc.ListPlans(f.ctx, f.userID)
	if err != nil || len(plans) != 0 {
		t.Fatalf("plans after a refused create = %d, %v", len(plans), err)
	}
}
