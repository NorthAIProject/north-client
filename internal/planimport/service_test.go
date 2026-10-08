package planimport_test

import (
	"bytes"
	"context"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/planimport"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

type goalLookup struct{ plan calculator.MacroPlan }

func (g goalLookup) Current(context.Context, uuid.UUID) (calculator.MacroPlan, error) {
	return g.plan, nil
}

type fixture struct {
	svc         *planimport.Service
	user        users.User
	workouts    *workouts.Service
	mealPlans   *meals.MealPlanService
	ingredients *meals.IngredientService
	model       *fake.Client
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: "importer@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Importer", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	model := fake.Text("")
	repo := meals.NewRepository(pool)
	f := fixture{
		user:        user,
		workouts:    workouts.NewService(workouts.Options{Repository: workouts.NewRepository(pool)}),
		mealPlans:   meals.NewMealPlanService(repo),
		ingredients: meals.NewIngredientService(repo),
		model:       model,
	}
	f.svc = planimport.NewService(planimport.Options{
		Workouts:    f.workouts,
		MealPlans:   f.mealPlans,
		Ingredients: f.ingredients,
		// Low carb is 20% of 200 g: a 40 g carb day.
		Goals: goalLookup{plan: calculator.MacroPlan{ProteinG: 150, FatG: 60, CarbG: 200}},
	})
	return f
}

func TestWorkoutCSVImportsWithoutInventedValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	csv := "Day,Exercise,Sets,Reps,Load,Rest,Notes\n" +
		"Day 1,Back Squat,3,5,100kg,180,brace\n" +
		",Leg Curl,,,,,\n" +
		"Thursday,Bench Press,4,6,RPE 8,2 min,\n"

	draft, err := f.svc.ParseWorkout(ctx, f.user, "block.csv", []byte(csv))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Saving before "Day 1" has a weekday is refused, and stores nothing.
	if _, err := f.svc.CommitWorkout(ctx, f.user, draft); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("commit with an unassigned day: err = %v, want validation", err)
	}

	draft.Days[0].Weekday = "Monday"
	draft.Name = "Block 3"
	draft.Days[0].Exercises[1].Name = "Lying Leg Curl" // a fix made on the review screen

	stored, err := f.svc.CommitWorkout(ctx, f.user, draft)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if stored.Source != workouts.SourceImported || stored.Plan.Name != "Block 3" {
		t.Fatalf("stored = %+v", stored)
	}

	mon := stored.Plan.Days[0]
	if mon.Weekday != "Monday" || mon.Focus != "Day 1" {
		t.Fatalf("day = %+v, want the label kept as the focus", mon)
	}
	curl := mon.Exercises[1]
	if curl.Name != "Lying Leg Curl" || curl.Sets != 0 || curl.Reps != "" || curl.Load != "" || curl.RestSeconds != 0 {
		t.Fatalf("curl = %+v, want nothing invented", curl)
	}
	bench := stored.Plan.Days[1].Exercises[0]
	if bench.Sets != 4 || bench.RestSeconds != 120 || bench.Load != "RPE 8" {
		t.Fatalf("bench = %+v", bench)
	}
	if len(f.model.Calls()) != 0 {
		t.Fatalf("a CSV import called the model")
	}
}

func TestMealCSVPreviewsAgainstTheTargetAndCannotSilentlySaveAnOverage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	rice, err := f.ingredients.Create(ctx, f.user.ID, meals.IngredientInput{
		Name: "White rice", Category: meals.CategoryCarb,
		Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28},
	})
	if err != nil {
		t.Fatal(err)
	}

	csv := "Day,Meal,Food,Quantity,Unit,Protein,Carbs,Fat\n" +
		"Monday,Lunch,White rice,100,g,,,\n" +
		"Tuesday,Lunch,White rice,0.2,kg,,,\n" +
		"Tuesday,Dinner,Coach's shake,300,g,72,6,6\n" +
		"Tuesday,Dinner,Mystery stew,1,bowl,,,\n"

	draft, err := f.svc.ParseMeal(ctx, f.user, "week.csv", []byte(csv))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if draft.Days[0].Meals[0].Foods[0].IngredientID == nil || *draft.Days[0].Meals[0].Foods[0].IngredientID != rice.ID {
		t.Fatalf("rice was not matched to the catalog: %+v", draft.Days[0].Meals[0].Foods[0])
	}

	// No plan type yet: nothing to measure against, totals only.
	if draft.Days[0].Target != nil || draft.Days[0].Totals == nil {
		t.Fatalf("day = %+v", draft.Days[0])
	}

	draft.PlanType = meals.PlanTypeLowCarb
	draft = f.svc.PreviewMeal(ctx, f.user.ID, draft)

	tue := draft.Days[1]
	if tue.Target == nil || tue.Target.CarbG != 40 {
		t.Fatalf("Tuesday target = %+v, want 40 g carbs", tue.Target)
	}
	if len(tue.Over) != 1 || !strings.Contains(tue.Over[0], "carbs over by") {
		t.Fatalf("Tuesday over = %q", tue.Over)
	}
	stew := tue.Meals[1].Foods[1]
	if stew.Ready() || len(stew.Checks) == 0 {
		t.Fatalf("stew = %+v, want it blocked with a reason", stew)
	}

	// Unresolved lines block the save.
	if _, _, err := f.svc.CommitMeal(ctx, f.user, draft, true); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("commit with an unresolved food: err = %v", err)
	}

	// The person deletes the stew and keeps the shake as their own food.
	tue.Meals[1].Foods = tue.Meals[1].Foods[:1]
	tue.Meals[1].Foods[0].SaveAsMine = true
	draft.Days[1] = tue

	_, previewed, err := f.svc.CommitMeal(ctx, f.user, draft, false)
	var over meals.ImportOverageError
	if !apperr.As(err, &over) {
		t.Fatalf("commit over target without confirming: err = %v, want an overage refusal", err)
	}
	if len(previewed.Days[1].Over) == 0 {
		t.Fatalf("the refused commit must hand back the overage to show")
	}
	if plans, _ := f.mealPlans.ListPlans(ctx, f.user.ID); len(plans) != 0 {
		t.Fatalf("a refused commit stored a plan")
	}

	saved, _, err := f.svc.CommitMeal(ctx, f.user, draft, true)
	if err != nil {
		t.Fatalf("confirmed commit: %v", err)
	}
	if len(saved.Meals) != 3 {
		t.Fatalf("meals = %d, want 3", len(saved.Meals))
	}
	for _, m := range saved.Meals {
		if m.Name == "Dinner" && (m.Ingredients[0].QuantityGrams != 300 || m.TotalMacros.ProteinG < 71.9) {
			t.Fatalf("shake = %+v", m.Ingredients)
		}
	}
}

// The web round trip: upload a sheet, see it on the review page, fix it, save.
// The quota guard is the router's concern and is not mounted here.
func TestWebWorkoutImportRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := auth.ContextWithUser(context.Background(), f.user)
	h := planimport.NewHandler(f.svc, nil)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "block.csv")
	_, _ = part.Write([]byte("Day,Exercise,Sets,Reps\nDay 1,Back Squat,3,5\n,Leg Curl,,\n"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/app/training/import", &body).WithContext(ctx)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.WorkoutParse(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Back Squat") {
		t.Fatalf("parse: %d\n%s", rec.Code, rec.Body.String())
	}

	draftJSON := hiddenDraft(t, rec.Body.String())
	form := url.Values{
		"draft":      {draftJSON},
		"name":       {"Block 3"},
		"d0.weekday": {"Tuesday"},
		"d0.e0.name": {"Back Squat"},
		"d0.e0.sets": {"3"},
		"d0.e0.reps": {"5"},
		"d0.e1.name": {"Leg Curl"},
		"d0.e1.sets": {""},
		"action":     {"delete-exercise:0:1"},
	}
	rec = postForm(t, ctx, h.WorkoutReview, "/app/training/import/review", form)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Leg Curl") {
		t.Fatalf("delete: %d, Leg Curl still shown", rec.Code)
	}

	form.Set("draft", hiddenDraft(t, rec.Body.String()))
	form.Del("d0.e1.name")
	form.Set("action", "save")
	rec = postForm(t, ctx, h.WorkoutReview, "/app/training/import/review", form)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/app/training/") {
		t.Fatalf("save: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	plans, err := f.workouts.ListPlans(context.Background(), f.user.ID, 5)
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans = %d, err %v", len(plans), err)
	}
	p := plans[0].Plan
	if p.Name != "Block 3" || p.Days[0].Weekday != "Tuesday" || len(p.Days[0].Exercises) != 1 {
		t.Fatalf("plan = %+v", p)
	}
}

func hiddenDraft(t *testing.T, page string) string {
	t.Helper()
	m := regexp.MustCompile(`name="draft" value="([^"]*)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no draft field on the page")
	}
	return html.UnescapeString(m[1])
}

func postForm(t *testing.T, ctx context.Context, handler http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode())).WithContext(ctx)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}
