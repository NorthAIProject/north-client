package planimport_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/planimport"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// generator is the fixture's service with a planner on the given model.
func generator(t *testing.T, f fixture, model *fake.Client) *planimport.Service {
	t.Helper()
	registry := ai.NewRegistry()
	registry.Register(model)
	runner := ai.NewRunner(registry, ai.NewChainSet([]string{model.Name()}, nil))
	return planimport.NewService(planimport.Options{
		Workouts:    f.workouts,
		MealPlans:   f.mealPlans,
		Ingredients: f.ingredients,
		Planner:     planimport.NewAIMealPlanner(runner, "", f.ingredients.SharedCatalog),
	})
}

// testFoods are personal ingredients with round macros, named so nothing in
// the shared catalog matches them as well.
func testFoods(t *testing.T, f fixture) {
	t.Helper()
	for _, in := range []meals.IngredientInput{
		{Name: "Test chicken", Category: meals.CategoryProtein, Per100g: meal.MacrosFromGrams(31, 3.6, 0)},
		{Name: "Test rice", Category: meals.CategoryCarb, Per100g: meal.MacrosFromGrams(2.7, 0.3, 28)},
		{Name: "Test oil", Category: meals.CategoryFat, Per100g: meal.MacrosFromGrams(0, 100, 0)},
		{Name: "Test yogurt", Category: meals.CategoryProtein, Per100g: meal.MacrosFromGrams(10, 0.4, 3.6)},
	} {
		if _, err := f.ingredients.Create(context.Background(), f.user.ID, in); err != nil {
			t.Fatal(err)
		}
	}
}

// planReply is one day of two meals. The fixture's mid-carb target is
// 150 g protein, 71 g carbs and 60 g fat; rice decides whether it fits.
func planReply(lunchRice float64, snack string) string {
	return fmt.Sprintf(`{"name":"Lean week","rationale":"Protein at every meal.","days":[{"label":"Day 1","meals":[
		{"name":"Lunch","foods":[{"food":"Test chicken","grams":250},{"food":"Test rice","grams":%g}],"alternatives":[]},
		{"name":"Dinner","foods":[{"food":"Test chicken","grams":210},{"food":"Test rice","grams":120},{"food":"Test oil","grams":35},{"food":%q,"grams":100}],"alternatives":[]}
	]}]}`, lunchRice, snack)
}

func TestGeneratedMealPlanIsCorrectedUntilItFitsAndSavesLikeAnImport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	testFoods(t, f)

	model := &fake.Client{Responses: []fake.Response{
		{Text: planReply(200, "Test yogurt")}, // over on carbs, and yogurt is dairy
		{Text: planReply(120, "Test chicken")},
	}}
	model.Responses[1].Text = strings.Replace(model.Responses[1].Text, `{"food":"Test chicken","grams":100}`, `{"food":"Test rice","grams":5}`, 1)
	svc := generator(t, f, model)

	draft, err := svc.GenerateMeal(ctx, f.user, meals.MealPlanIntake{
		Objective: "cutting", Days: 1, MealsPerDay: 2, PlanType: meal.MidCarb,
		Exclusions: []string{"Dairy", "dairy", " "}, Notes: "quick lunches",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	calls := model.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want a draft and one correction", len(calls))
	}
	if !strings.Contains(calls[0].System, "Almonds: 21 / 21.3 / 52.7") {
		t.Error("the shared catalog's macros are not in the system prompt")
	}
	if first := calls[0].Messages[0].Parts[0].Text; strings.Contains(calls[0].System, "Never use these") ||
		!strings.Contains(first, "Never use these, or anything made from them: Dairy.") ||
		!strings.Contains(first, "protein 150 g, carbs 71 g, fat 60 g") || !strings.Contains(first, "quick lunches") {
		t.Errorf("the person's request belongs in the user message, not the cached system prompt:\n%s", first)
	}
	correction := calls[1].Messages[len(calls[1].Messages)-1].Parts[0].Text
	for _, want := range []string{`"Test yogurt" in Day 1, Dinner is Dairy`} {
		if !strings.Contains(correction, want) {
			t.Errorf("correction lacks %q:\n%s", want, correction)
		}
	}

	if !draft.HasTarget || !draft.EveryDay || draft.Mode != string(meal.Easy) || draft.Notes != "Protein at every meal." {
		t.Fatalf("draft = %+v", draft)
	}
	day := draft.Days[0]
	if len(day.Over) != 0 || day.Totals.CarbG > day.Target.CarbG+0.5 {
		t.Fatalf("day = totals %+v target %+v over %q", day.Totals, day.Target, day.Over)
	}

	last, err := f.mealPlans.LatestIntake(ctx, f.user.ID)
	if err != nil || last == nil {
		t.Fatalf("intake not saved: %v", err)
	}
	if strings.Join(last.Exclusions, "|") != "Dairy" || last.MealsPerDay != 2 || last.Notes != "quick lunches" {
		t.Fatalf("saved intake = %+v", last)
	}

	saved, _, err := svc.CommitMeal(ctx, f.user, draft, false)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(saved.Days) != 7 || len(saved.Days[0].Meals) != 2 {
		t.Fatalf("saved %d days, first with %d meals; one generated day is eaten every day", len(saved.Days), len(saved.Days[0].Meals))
	}
}

// The overage check runs only once the shape is right: a plan a day short
// is sent back for that, not for macros it was never going to fit.
func TestGeneratedMealPlanThatNeverFitsIsRefusedNotSaved(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	testFoods(t, f)

	model := fake.Text(planReply(400, "Test chicken"))
	svc := generator(t, f, model)

	_, err := svc.GenerateMeal(ctx, f.user, meals.MealPlanIntake{Objective: "maintenance", Days: 1, MealsPerDay: 2, PlanType: meal.MidCarb})
	if !apperr.Is(err, apperr.ErrUnavailable) || !strings.Contains(err.Error(), "over its target") {
		t.Fatalf("err = %v, want unavailable with the reason", err)
	}
	if n := len(model.Calls()); n != 3 {
		t.Fatalf("calls = %d, want three attempts", n)
	}
	plans, err := f.mealPlans.ListPlans(ctx, f.user.ID)
	if err != nil || len(plans) != 0 {
		t.Fatalf("plans = %d, err = %v; nothing is saved without review", len(plans), err)
	}
}

func TestMealPlanCannotBeGeneratedWithoutATarget(t *testing.T) {
	f := newFixture(t)
	noTarget := meals.NewMealPlanService(f.repo, noGoal{})
	model := fake.Text(planReply(120, "Test chicken"))
	registry := ai.NewRegistry()
	registry.Register(model)
	svc := planimport.NewService(planimport.Options{
		MealPlans: noTarget, Ingredients: f.ingredients,
		Planner: planimport.NewAIMealPlanner(ai.NewRunner(registry, ai.NewChainSet([]string{model.Name()}, nil)), "", nil),
	})

	_, err := svc.GenerateMeal(context.Background(), f.user, meals.MealPlanIntake{Objective: "cutting", Days: 1, MealsPerDay: 3, PlanType: meal.LowCarb})
	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) || fields.Messages()["macro_target"] == "" {
		t.Fatalf("err = %v, want a macro_target field error", err)
	}
	if len(model.Calls()) != 0 {
		t.Fatal("the model was asked for a plan nobody can save")
	}
}

func TestWebMealGenerateFormRemembersAnswersAndShowsTheReview(t *testing.T) {
	f := newFixture(t)
	testFoods(t, f)
	ctx := auth.ContextWithUser(context.Background(), f.user)
	reply := strings.Replace(planReply(120, "Test chicken"), `{"food":"Test chicken","grams":100}`, `{"food":"Test rice","grams":5}`, 1)
	h := planimport.NewHandler(generator(t, f, fake.Text(reply)), nil)

	req := httptest.NewRequest(http.MethodGet, "/app/nutrition/plans/generate", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.MealGenerateForm(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="/app/nutrition/plans/generate"`) {
		t.Fatalf("form: %d\n%s", rec.Code, rec.Body.String())
	}

	form := url.Values{
		"objective": {"cutting"}, "days": {"1"}, "meals_per_day": {"2"}, "plan_type": {"mid_carb"},
		"exclusions": {"dairy, peanuts"}, "notes": {"quick lunches"},
	}
	rec = postForm(t, ctx, h.MealGenerate, "/app/nutrition/plans/generate", form)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Lean week") || hiddenDraft(t, rec.Body.String()) == "" {
		t.Fatalf("generate: %d\n%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.MealGenerateForm(rec, req)
	if !strings.Contains(rec.Body.String(), "dairy, peanuts") || !strings.Contains(rec.Body.String(), "quick lunches") {
		t.Fatalf("the form does not remember the last answers:\n%s", rec.Body.String())
	}

	form.Set("meals_per_day", "9")
	rec = postForm(t, ctx, h.MealGenerate, "/app/nutrition/plans/generate", form)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Choose between 1 and 8 meals a day.") {
		t.Fatalf("invalid: %d\n%s", rec.Code, rec.Body.String())
	}
}
