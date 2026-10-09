package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/planimport"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// memStorage keeps uploaded chat files in memory.
type memStorage struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (m *memStorage) Put(_ context.Context, key, _ string, body io.Reader) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = b
	return nil
}

func (m *memStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *memStorage) SignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://storage.test/" + key, nil
}

// mealReader answers every meal read with the same reading. No model.
type mealReader struct {
	reading planimport.MealReading
	hint    string
}

func (r *mealReader) ReadWorkout(context.Context, users.User, planimport.Source, string) (string, []planimport.WorkoutRow, []string, error) {
	return "", nil, nil, errors.New("mealReader reads meals only")
}

func (r *mealReader) ReadMeal(_ context.Context, _ users.User, _ planimport.Source, hint string) (planimport.MealReading, error) {
	r.hint = hint
	return r.reading, nil
}

// refusingQuota has no plan imports left.
type refusingQuota struct{}

func (refusingQuota) Consume(context.Context, uuid.UUID, string, quota.Action) (quota.Decision, error) {
	return quota.Decision{Allowed: false, RetryAfter: 20 * time.Minute}, nil
}

// noTarget is a calculator with no macro target worked out.
type noTarget struct{}

func (noTarget) Current(context.Context, uuid.UUID) (calculator.MacroPlan, error) {
	return calculator.MacroPlan{}, apperr.ErrNotFound
}

type importFixture struct {
	user      users.User
	files     *media.Service
	plans     *meals.MealPlanService
	documents *documents.Service
	reader    *mealReader
	registry  *Registry
	services  Services
}

func newImportFixture(t *testing.T) importFixture {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email: "import@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "T", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}

	mealsRepo := meals.NewRepository(pool)
	ingredientSvc := meals.NewIngredientService(mealsRepo)
	for _, in := range []meals.IngredientInput{
		{Name: "Grilled chicken breast", Category: meals.CategoryProtein, Per100g: meals.Macros{Calories: 165, ProteinG: 31, FatG: 3.6}},
		{Name: "Plain white rice", Category: meals.CategoryCarb, Per100g: meals.Macros{Calories: 130, ProteinG: 2.7, FatG: 0.3, CarbG: 28}},
	} {
		if _, err = ingredientSvc.Create(ctx, user.ID, in); err != nil {
			t.Fatalf("ingredient: %v", err)
		}
	}
	planSvc := meals.NewMealPlanService(mealsRepo, fixedTarget{ProteinG: 150, FatG: 70, CarbG: 250})
	foodLog := meals.NewFoodLogService(mealsRepo)
	reader := &mealReader{}
	importSvc := planimport.NewService(planimport.Options{
		Reader: reader, MealPlans: planSvc, Ingredients: ingredientSvc,
	})
	files := media.NewService(media.Options{Repository: media.NewRepository(pool), Storage: &memStorage{objs: map[string][]byte{}}})
	docs := documents.NewService(documents.NewRepository(pool), nil, nil)

	svc := Services{
		Users: userSvc, Ingredients: ingredientSvc, MealPlans: planSvc, FoodLog: foodLog,
		Documents: docs, PlanImport: importSvc, Media: files,
	}
	return importFixture{
		user: user, files: files, plans: planSvc, documents: docs, reader: reader,
		registry: Build(svc), services: svc,
	}
}

func (f importFixture) attach(t *testing.T, name, body string) {
	t.Helper()
	if _, err := f.files.UploadFile(context.Background(), f.user.ID, name, int64(len(body)), strings.NewReader(body)); err != nil {
		t.Fatalf("attach %s: %v", name, err)
	}
}

func (f importFixture) call(t *testing.T, name string, args any) ai.ToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return f.registry.Invoke(context.Background(), f.user.ID, ai.ToolCall{Name: name, Arguments: raw})
}

func (f importFixture) invoke(t *testing.T, name string, args any) string {
	t.Helper()
	res := f.call(t, name, args)
	if res.IsError {
		t.Fatalf("%s: %s", name, res.Content)
	}
	return res.Content
}

// everyDayLunch is a plan file with no weekdays: breakfast, and a lunch with
// three options, the last of which matches no food.
func everyDayLunch() planimport.MealReading {
	return planimport.MealReading{
		Name: "Dieta Ana",
		Rows: []planimport.MealRow{
			{Meal: "Breakfast", Food: "Plain white rice", Quantity: "50", Unit: "g"},
			{Meal: "Almoço", Option: "Frango", Food: "Grilled chicken breast", Quantity: "150", Unit: "g"},
			{Meal: "Almoço", Option: "Arroz", Food: "Plain white rice", Quantity: "100", Unit: "g"},
			{Meal: "Almoço", Option: "Arroz", Food: "Grilled chicken breast", Quantity: "50", Unit: "g"},
			{Meal: "Almoço", Option: "Sopa", Food: "Zzqx stew", Quantity: "1", Unit: "bowl"},
		},
		Notes: "Drink 2 L of water a day.",
	}
}

func TestImportPlanFromAttachmentSavesAndSummarises(t *testing.T) {
	f := newImportFixture(t)
	f.reader.reading = everyDayLunch()
	f.attach(t, "dieta.txt", "Almoço: frango ou arroz")

	out := f.invoke(t, "import_plan_from_attachment", map[string]any{
		"kind": "meal", "file": "Dieta.TXT", "plan_type": "high_carb",
		"instructions": "skip the soup",
	})

	for _, want := range []string{
		`"Dieta Ana"`,
		"every day",
		"Almoço: 2 options; option 1 counted",
		"Breakfast: 1 option",
		"Zzqx stew",
		"Saved the plan's advice and recipes to their notes.",
		"edit_meal_plan",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
	if f.reader.hint != "skip the soup" {
		t.Errorf("hint = %q, want the instructions", f.reader.hint)
	}

	plans, err := f.plans.ListPlans(context.Background(), f.user.ID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans = %d, %v", len(plans), err)
	}
	if plans[0].Settings.Type != "high_carb" {
		t.Errorf("plan type = %s, want high_carb", plans[0].Settings.Type)
	}

	docs, err := f.documents.List(context.Background(), f.user.ID)
	if err != nil || len(docs) != 1 || !strings.Contains(docs[0].Title, "Dieta Ana") {
		t.Fatalf("notes = %+v, %v; want one note named after the plan", docs, err)
	}
}

func TestImportPlanFromAttachmentWithoutDocumentsSavesNoNotes(t *testing.T) {
	f := newImportFixture(t)
	f.services.Documents = nil
	r := Build(f.services)
	f.registry = r
	f.reader.reading = everyDayLunch()
	f.attach(t, "dieta.txt", "Almoço")

	out := f.invoke(t, "import_plan_from_attachment", map[string]any{"kind": "meal", "file": "dieta.txt"})
	if strings.Contains(out, "notes") {
		t.Errorf("result mentions notes with no documents wired:\n%s", out)
	}
}

func TestImportPlanFromAttachmentExplainsWhatStoppedIt(t *testing.T) {
	f := newImportFixture(t)
	f.reader.reading = everyDayLunch()

	// Nothing by that name was sent.
	res := f.call(t, "import_plan_from_attachment", map[string]any{"kind": "meal", "file": "dieta.pdf"})
	if !res.IsError || !strings.Contains(res.Content, `"dieta.pdf"`) || !strings.Contains(res.Content, "attach it again") {
		t.Errorf("a missing file = %+v, want a friendly not-found", res)
	}

	// No file named at all.
	res = f.call(t, "import_plan_from_attachment", map[string]any{"kind": "meal"})
	if !res.IsError || !strings.Contains(res.Content, "file") {
		t.Errorf("no file name = %+v, want a refusal asking for it", res)
	}

	// A file nothing in which matches a food.
	f.reader.reading = planimport.MealReading{Rows: []planimport.MealRow{{Meal: "Lunch", Food: "Zzqx stew", Quantity: "1", Unit: "bowl"}}}
	f.attach(t, "stew.txt", "Lunch: stew")
	res = f.call(t, "import_plan_from_attachment", map[string]any{"kind": "meal", "file": "stew.txt"})
	if !res.IsError || !strings.Contains(res.Content, "could be matched") {
		t.Errorf("nothing matched = %+v", res)
	}

	// A file too long to import.
	f.attach(t, "long.txt", strings.Repeat("a", planimport.MaxTextChars+1))
	res = f.call(t, "import_plan_from_attachment", map[string]any{"kind": "meal", "file": "long.txt"})
	if !res.IsError || !strings.Contains(res.Content, "too long to import") || !strings.Contains(res.Content, "long.txt") {
		t.Errorf("too long = %+v", res)
	}
	f.reader.reading = everyDayLunch()

	// The plan imports are spent.
	f.services.PlanImport = planimport.NewService(planimport.Options{
		Reader: f.reader, MealPlans: f.plans, Ingredients: f.services.Ingredients, Quota: refusingQuota{},
	})
	f.registry = Build(f.services)
	f.attach(t, "dieta.txt", "Almoço")
	res = f.call(t, "import_plan_from_attachment", map[string]any{"kind": "meal", "file": "dieta.txt"})
	if !res.IsError || !strings.Contains(res.Content, "plan imports") || !strings.Contains(res.Content, "20 minutes") {
		t.Errorf("quota spent = %+v, want the retry time", res)
	}
}

func TestImportPlanFromAttachmentNeedsATargetForMeals(t *testing.T) {
	f := newImportFixture(t)
	f.reader.reading = everyDayLunch()
	f.services.PlanImport = planimport.NewService(planimport.Options{
		Reader: f.reader, Ingredients: f.services.Ingredients,
		MealPlans: meals.NewMealPlanService(meals.NewRepository(nil), noTarget{}),
	})
	f.registry = Build(f.services)
	f.attach(t, "dieta.txt", "Almoço")

	res := f.call(t, "import_plan_from_attachment", map[string]any{"kind": "meal", "file": "dieta.txt"})
	if !res.IsError || !strings.Contains(res.Content, "nutrition target first") {
		t.Errorf("no target = %+v, want them told to set a nutrition target first", res)
	}
}

func TestImportPlanFromAttachmentIsRegisteredOnlyWithItsServices(t *testing.T) {
	f := newImportFixture(t)
	if _, ok := f.registry.capabilities["import_plan_from_attachment"]; !ok {
		t.Fatal("import_plan_from_attachment is missing with every service wired")
	}
	if f.registry.capabilities["import_plan_from_attachment"].ReadOnly {
		t.Error("the import writes a plan, so it must show an approval card")
	}
	for name, drop := range map[string]func(*Services){
		"PlanImport": func(s *Services) { s.PlanImport = nil },
		"Media":      func(s *Services) { s.Media = nil },
		"Users":      func(s *Services) { s.Users = nil },
	} {
		s := f.services
		drop(&s)
		if _, ok := Build(s).capabilities["import_plan_from_attachment"]; ok {
			t.Errorf("registered without %s", name)
		}
	}
}

func TestMealPlanToolsWorkWithOptions(t *testing.T) {
	f := newImportFixture(t)
	f.reader.reading = everyDayLunch()
	f.attach(t, "dieta.txt", "Almoço")
	f.invoke(t, "import_plan_from_attachment", map[string]any{"kind": "meal", "file": "dieta.txt"})

	// Seven identical days are described once.
	out := f.invoke(t, "get_meal_plan", map[string]any{"plan": ""})
	if !strings.Contains(out, "Every day (Monday–Sunday)") || strings.Count(out, "Breakfast") != 1 ||
		!strings.Contains(out, `option 2 "Arroz"`) {
		t.Fatalf("get_meal_plan:\n%s", out)
	}

	// Option 2 gains food on Monday only; a third option is added and removed.
	out = f.invoke(t, "edit_meal_plan", map[string]any{"changes": []map[string]any{
		{"op": "add_food", "days": []string{"Monday"}, "meal": "Almoço", "option": 2, "food": "Plain white rice", "grams": 30},
		{"op": "add_option", "days": []string{"Monday"}, "meal": "Almoço", "option_label": "Peixe"},
	}})
	if !strings.Contains(out, "Almoço · Arroz") || !strings.Contains(out, "Almoço · Peixe") {
		t.Errorf("edit_meal_plan said:\n%s", out)
	}
	out = f.invoke(t, "get_meal_plan", map[string]any{"plan": ""})
	if strings.Contains(out, "Every day (Monday–Sunday)") || !strings.Contains(out, "\nMonday: ") ||
		!strings.Contains(out, `option 2 "Arroz"`) || !strings.Contains(out, "30 g Plain white rice") ||
		!strings.Contains(out, `option 3 "Peixe" (0 kcal): nothing yet`) {
		t.Fatalf("after the Monday edit:\n%s", out)
	}
	f.invoke(t, "edit_meal_plan", map[string]any{"changes": []map[string]any{
		{"op": "remove_option", "days": []string{"Monday"}, "meal": "Almoço", "option": 3},
	}})
	if out = f.invoke(t, "get_meal_plan", map[string]any{"plan": ""}); strings.Contains(out, "Peixe") {
		t.Errorf("option 3 survived remove_option:\n%s", out)
	}

	// An option the meal does not have refuses the change.
	if res := f.call(t, "edit_meal_plan", map[string]any{"changes": []map[string]any{
		{"op": "set_grams", "meal": "Almoço", "option": 5, "food": "Plain white rice", "grams": 10},
	}}); !res.IsError || !strings.Contains(res.Content, "no option 5") {
		t.Errorf("option 5 = %+v", res)
	}

	// Logging by label and by number takes that option's food.
	out = f.invoke(t, "log_planned_meal", map[string]any{"meal": "almoço", "option": "arroz"})
	if !strings.Contains(out, "Almoço · Arroz") {
		t.Errorf("log by label said %q", out)
	}
	out = f.invoke(t, "log_planned_meal", map[string]any{"meal": "almoço", "option": "1"})
	if !strings.Contains(out, "Almoço · Frango") {
		t.Errorf("log by number said %q", out)
	}
}

func TestExplainImportErrorKeepsWhatThePersonCanActOn(t *testing.T) {
	busy := explainImportError(&planimport.FileError{Reason: planimport.ReasonBusy, Message: "One import at a time."}, "dieta.pdf")
	if !apperr.Is(busy, apperr.ErrConflict) || !strings.Contains(busy.Error(), "still running") {
		t.Errorf("busy = %v", busy)
	}
	unsupported := explainImportError(&planimport.FileError{Reason: planimport.ReasonUnsupported, Message: "This kind of file can't be read."}, "plan.exe")
	if !apperr.Is(unsupported, apperr.ErrValidation) || !strings.Contains(unsupported.Error(), "plan.exe could not be imported: This kind of file can't be read.") {
		t.Errorf("unsupported = %v", unsupported)
	}
	outage := errors.New("connection refused")
	if got := explainImportError(outage, "dieta.pdf"); got != outage {
		t.Errorf("an outage was rewritten: %v", got)
	}
}
