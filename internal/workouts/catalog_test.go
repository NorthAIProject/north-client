package workouts_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

var errBoom = errors.New("the catalog is unreachable")

// stubCatalog is a fixed catalog. A stub rather than the real service because
// these tests are about what the workouts service does with catalog rows, not
// about how they are stored — and because a stub can be made to fail on
// demand, which a seeded database cannot.
type stubCatalog struct {
	rows         []exercise.Exercise
	candidateErr error
	resolveErr   error

	candidateEquipment []string
	resolvedSlugs      []string
}

func (c *stubCatalog) Candidates(_ context.Context, equipment []string) ([]exercise.Exercise, error) {
	c.candidateEquipment = equipment
	if c.candidateErr != nil {
		return nil, c.candidateErr
	}
	return c.rows, nil
}

func (c *stubCatalog) Resolve(_ context.Context, slugs []string) (map[string]exercise.Exercise, error) {
	c.resolvedSlugs = slugs
	if c.resolveErr != nil {
		return nil, c.resolveErr
	}

	found := map[string]exercise.Exercise{}
	for _, slug := range slugs {
		for _, row := range c.rows {
			if row.Slug == strings.ToLower(slug) {
				found[row.Slug] = row
			}
		}
	}
	return found, nil
}

// SearchByName is the swap panel's picker. The stub matches on a substring of
// the name, which is what the real catalog's ILIKE does, and ignores equipment:
// these tests are about what the workouts service does with the rows, not about
// re-testing the catalog's own filter.
func (c *stubCatalog) SearchByName(_ context.Context, query string, _ []string, limit int) ([]exercise.Exercise, error) {
	if c.resolveErr != nil {
		return nil, c.resolveErr
	}

	var found []exercise.Exercise
	for _, row := range c.rows {
		if len(found) == limit {
			break
		}
		if query == "" || strings.Contains(strings.ToLower(row.Name), strings.ToLower(query)) {
			found = append(found, row)
		}
	}
	return found, nil
}

func gobletSquat() exercise.Exercise {
	return exercise.Exercise{
		Slug: "dumbbell-goblet-squat", Name: "Dumbbell Goblet Squat",
		Category: exercise.CategoryStrength, Equipment: "dumbbell", Difficulty: exercise.DifficultyBeginner,
		Primary: []string{"quads"}, Secondary: []string{"glutes", "abs"},
	}
}

func newServiceWithCatalog(t *testing.T, client *fake.Client, catalog workouts.Catalog) (*workouts.Service, users.User) {
	t.Helper()

	svc, user, _ := newServiceWithCatalogAndPool(t, client, catalog)
	return svc, user
}

// newServiceWithCatalogAndPool also hands back the database, for tests that
// need a second account to prove one person's plans stay theirs.
func newServiceWithCatalogAndPool(t *testing.T, client *fake.Client, catalog workouts.Catalog) (*workouts.Service, users.User, *pgxpool.Pool) {
	t.Helper()

	pool := testdb.New(t)

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	registry := ai.NewRegistry()
	registry.Register(client)
	runtime := ai.NewRunner(registry, ai.NewChainSet([]string{client.Name()}, nil))

	return workouts.NewService(workouts.Options{
		Repository: workouts.NewRepository(pool),
		Runner:     runtime,
		Catalog:    catalog,
		Model:      "test-model",
	}), user, pool
}

// planCitingCatalog is a valid plan whose first exercise claims a catalog slug
// and carries deliberately wrong muscle keys, so a test can tell whether the
// catalog overwrote them.
func planCitingCatalog(slug string) workouts.Plan {
	cited := workouts.Exercise{
		Name: "Dumbbell Goblet Squat", CatalogSlug: slug,
		Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbell",
		Primary: []string{"biceps"}, Secondary: []string{"calves"}, Stabilizers: []string{"abs"},
	}

	return workouts.Plan{
		Name: "Dumbbell foundations", Rationale: "Three full-body days suit a beginner.", WeeksTotal: 8,
		Days: []workouts.PlanDay{
			{Weekday: "Monday", Focus: "full body", Exercises: []workouts.Exercise{cited}},
			{Weekday: "Wednesday", Focus: "full body", Exercises: []workouts.Exercise{
				{Name: "Push-up", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "none", Primary: []string{"chest"}},
			}},
			{Weekday: "Friday", Focus: "full body", Exercises: []workouts.Exercise{
				{Name: "Push-up", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "none", Primary: []string{"chest"}},
			}},
		},
	}
}

// The whole point of the catalog: a curated answer to "what does this train"
// replaces the generated one.
func TestCatalogMusclesReplaceTheModelsForResolvedExercises(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)

	plan, _, err := svc.Generate(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	got := plan.Days[0].Exercises[0]
	if want := []string{"quads"}; !equalStrings(got.Primary, want) {
		t.Errorf("Primary = %v, want %v from the catalog", got.Primary, want)
	}
	if want := []string{"glutes", "abs"}; !equalStrings(got.Secondary, want) {
		t.Errorf("Secondary = %v, want %v from the catalog", got.Secondary, want)
	}
	// The catalog carries no stabilizers, so the model's survive rather than
	// being blanked by a field the catalog has no opinion about.
	if want := []string{"abs"}; !equalStrings(got.Stabilizers, want) {
		t.Errorf("Stabilizers = %v, want the model's %v", got.Stabilizers, want)
	}
}

// The free-text fallback. An exercise the catalog does not carry is still a
// valid part of a plan; it just keeps the model's own keys.
func TestUnresolvedExercisesKeepTheModelsMuscles(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("an-exercise-the-model-invented"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)

	plan, _, err := svc.Generate(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	got := plan.Days[0].Exercises[0]
	if want := []string{"biceps"}; !equalStrings(got.Primary, want) {
		t.Errorf("Primary = %v, want the model's %v", got.Primary, want)
	}
	// Blanked so nothing downstream treats an invented slug as a real
	// catalog reference.
	if got.CatalogSlug != "" {
		t.Errorf("CatalogSlug = %q, want it cleared once it failed to resolve", got.CatalogSlug)
	}
}

func TestCandidatesAreOfferedToTheModel(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)

	if _, _, err := svc.Generate(context.Background(), user, dumbbellIntake()); err != nil {
		t.Fatalf("generate: %v", err)
	}

	calls := client.Calls()
	if len(calls) == 0 {
		t.Fatal("the model was never called")
	}

	system := calls[0].System
	if !strings.Contains(system, "dumbbell-goblet-squat") {
		t.Errorf("the candidate's slug is missing from the prompt, so the model cannot cite it:\n%s", system)
	}
	if !strings.Contains(system, "catalog_slug") {
		t.Error("the prompt never tells the model what to do with the slug")
	}
	if !equalStrings(catalog.candidateEquipment, []string{"dumbbell"}) {
		t.Errorf("candidates were fetched for %v, want the intake's equipment", catalog.candidateEquipment)
	}
}

// The catalog improves a plan; it is not required to produce one. A database
// hiccup should cost the person their muscle highlighting, not their programme.
func TestGenerationSurvivesACatalogFailure(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		catalog *stubCatalog
	}{
		{"candidates fail", &stubCatalog{candidateErr: errBoom}},
		{"resolve fails", &stubCatalog{rows: []exercise.Exercise{gobletSquat()}, resolveErr: errBoom}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &fake.Client{Responses: []fake.Response{
				{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
			}}
			svc, user := newServiceWithCatalog(t, client, tt.catalog)

			plan, _, err := svc.Generate(context.Background(), user, dumbbellIntake())
			if err != nil {
				t.Fatalf("a catalog failure must not fail the generation: %v", err)
			}
			if len(plan.Days) != 3 {
				t.Fatalf("got %d days, want the plan through intact", len(plan.Days))
			}
			// The model's own keys stand, because nothing could overwrite them.
			if want := []string{"biceps"}; !equalStrings(plan.Days[0].Exercises[0].Primary, want) {
				t.Errorf("Primary = %v, want the model's %v", plan.Days[0].Exercises[0].Primary, want)
			}
		})
	}
}

// A nil catalog is the pre-catalog behaviour, and has to keep working: it is
// what every existing test constructs.
func TestGenerationWorksWithNoCatalogAtAll(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}
	svc, user := newServiceWithCatalog(t, client, nil)

	plan, _, err := svc.Generate(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if want := []string{"biceps"}; !equalStrings(plan.Days[0].Exercises[0].Primary, want) {
		t.Errorf("Primary = %v, want the model's %v untouched", plan.Days[0].Exercises[0].Primary, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The plan page is where someone reads a session mid-workout, so the artwork
// has to reach plan exercises, not only the catalog browser.
func TestCatalogIllustrationReachesTheGeneratedPlan(t *testing.T) {
	t.Parallel()

	row := gobletSquat()
	row.IllustrationSlug = "goblet-squat"

	catalog := &stubCatalog{rows: []exercise.Exercise{row}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)

	generated, _, err := svc.Generate(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	got := generated.Days[0].Exercises[0]
	if got.IllustrationSlug != "goblet-squat" {
		t.Errorf("IllustrationSlug = %q, want %q from the catalog row", got.IllustrationSlug, "goblet-squat")
	}
	if !got.HasIllustration() {
		t.Error("HasIllustration() is false despite a slug being set")
	}
}

// An exercise the model invented has no catalog row and so no artwork. It must
// come back empty rather than inheriting a neighbour's illustration.
func TestAnInventedExerciseGetsNoIllustration(t *testing.T) {
	t.Parallel()

	row := gobletSquat()
	row.IllustrationSlug = "goblet-squat"

	catalog := &stubCatalog{rows: []exercise.Exercise{row}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("an-exercise-the-model-invented"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)

	generated, _, err := svc.Generate(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if got := generated.Days[0].Exercises[0]; got.HasIllustration() {
		t.Errorf("an invented exercise carries illustration %q", got.IllustrationSlug)
	}
}

// The reason PlanForDisplay re-resolves the catalog instead of trusting the
// stored JSONB: applyCatalog runs at generation, so every plan built before the
// catalog carried artwork has no illustration_slug stored and never would.
//
// This generates a plan against a catalog with no artwork, then gives the
// catalog artwork and reads the plan back — which is exactly what happened to
// every plan already in the database.
func TestPlanForDisplayFillsInIllustrationsStoredWithoutThem(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	// CreatePlan rather than Generate: Generate does not persist, and this test
	// is about what a stored plan looks like when it is read back later.
	stored, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if stored.Plan.Days[0].Exercises[0].HasIllustration() {
		t.Fatal("the catalog had no artwork, so the stored plan should carry none")
	}

	// The catalog gains artwork after the plan was stored.
	withArt := gobletSquat()
	withArt.IllustrationSlug = "goblet-squat"
	catalog.rows = []exercise.Exercise{withArt}

	shown, _, err := svc.PlanForDisplay(ctx, stored.ID, user.ID)
	if err != nil {
		t.Fatalf("plan for display: %v", err)
	}

	if got := shown.Plan.Days[0].Exercises[0]; got.IllustrationSlug != "goblet-squat" {
		t.Errorf("IllustrationSlug = %q after re-reading, want the catalog's %q", got.IllustrationSlug, "goblet-squat")
	}

	// GetPlan is the path the dashboard and the coach's context use, and must
	// stay the cheap one — no catalog query, so no artwork.
	raw, err := svc.GetPlan(ctx, stored.ID, user.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if raw.Plan.Days[0].Exercises[0].HasIllustration() {
		t.Error("GetPlan applied the catalog; it should stay the query-free read")
	}
}

// Editing is append-only. The row count, the parent link and the untouched
// original are the whole reason the design works — an UPDATE would satisfy the
// UI just as well and quietly destroy the model's plan.
func TestSwappingInsertsANewPlanAndLeavesTheOriginalIntact(t *testing.T) {
	t.Parallel()

	squat := gobletSquat()
	press := exercise.Exercise{
		Slug: "dumbbell-bench-press", Name: "Dumbbell Bench Press",
		Category: exercise.CategoryStrength, Equipment: "dumbbell",
		Difficulty: exercise.DifficultyBeginner,
		Primary:    []string{"chest"}, Secondary: []string{"triceps"},
	}

	catalog := &stubCatalog{rows: []exercise.Exercise{squat, press}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	original, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	edited, err := svc.SwapExercise(ctx, user, original.ID, 0, 0, "dumbbell-bench-press")
	if err != nil {
		t.Fatalf("swap: %v", err)
	}

	if edited.ID == original.ID {
		t.Fatal("the edit reused the original row; it must insert a new one")
	}
	if edited.Source != workouts.SourceEdited {
		t.Errorf("Source = %q, want %q", edited.Source, workouts.SourceEdited)
	}
	if edited.EditedFrom == nil || *edited.EditedFrom != original.ID {
		t.Errorf("EditedFrom = %v, want the original's id", edited.EditedFrom)
	}
	if edited.IntakeID != original.IntakeID {
		t.Error("the edit did not carry the original's intake, which is NOT NULL")
	}
	// Model and provider record the generation this descends from, which is
	// still true after a human edit.
	if edited.Model != original.Model || edited.Provider != original.Provider {
		t.Error("the edit lost the generation it came from")
	}
	if got := edited.Plan.Days[0].Exercises[0].Name; got != "Dumbbell Bench Press" {
		t.Errorf("the swap did not apply: exercise is %q", got)
	}

	// The original must still be readable exactly as it was stored.
	reread, err := svc.GetPlan(ctx, original.ID, user.ID)
	if err != nil {
		t.Fatalf("re-read the original: %v", err)
	}
	if got := reread.Plan.Days[0].Exercises[0].Name; got != "Dumbbell Goblet Squat" {
		t.Errorf("the original was modified: exercise is now %q", got)
	}
	if reread.Source != workouts.SourceAI {
		t.Errorf("the original's Source = %q, want %q", reread.Source, workouts.SourceAI)
	}

	// And the edit is what /app/training now resolves to.
	newest, err := svc.LatestPlan(ctx, user.ID)
	if err != nil {
		t.Fatalf("latest plan: %v", err)
	}
	if newest.ID != edited.ID {
		t.Error("the edited plan is not the newest, so the app would still show the old one")
	}
}

// Two tabs, or a double-clicked button, would each fork from the same parent
// and one edit would vanish. Editing is append-only so nothing corrupts — but
// the change someone just watched happen would not be the plan they follow.
func TestEditingASupersededPlanIsRefused(t *testing.T) {
	t.Parallel()

	squat := gobletSquat()
	press := exercise.Exercise{
		Slug: "dumbbell-bench-press", Name: "Dumbbell Bench Press",
		Category: exercise.CategoryStrength, Equipment: "dumbbell",
		Difficulty: exercise.DifficultyBeginner, Primary: []string{"chest"},
	}

	catalog := &stubCatalog{rows: []exercise.Exercise{squat, press}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	original, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if _, err = svc.SwapExercise(ctx, user, original.ID, 0, 0, "dumbbell-bench-press"); err != nil {
		t.Fatalf("first swap: %v", err)
	}

	// The second edit still names the original, as a stale tab would.
	_, err = svc.SwapExercise(ctx, user, original.ID, 0, 0, "dumbbell-bench-press")
	if !apperr.Is(err, workouts.ErrPlanSuperseded) {
		t.Fatalf("second swap error = %v, want ErrPlanSuperseded", err)
	}

	plans, err := svc.ListPlans(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("found %d plans, want 2 — the refused edit must not have written a fork", len(plans))
	}
}

// A slug that is not in the catalog must not be written through as a plan
// exercise pointing at a row that does not exist.
func TestSwappingToAnUnknownExerciseIsRejected(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	original, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if _, err := svc.SwapExercise(ctx, user, original.ID, 0, 0, "not-a-real-exercise"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if _, err := svc.SwapExercise(ctx, user, original.ID, 0, 0, ""); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("empty slug error = %v, want ErrValidation", err)
	}
}

// Suggestions rank on shared muscles and never offer the lift already in the
// slot as a replacement for itself.
func TestSuggestedReplacementsShareMusclesAndExcludeTheCurrentExercise(t *testing.T) {
	t.Parallel()

	squat := gobletSquat() // quads / glutes, dumbbell
	frontSquat := exercise.Exercise{
		Slug: "dumbbell-front-squat", Name: "Dumbbell Front Squat",
		Category: exercise.CategoryStrength, Equipment: "dumbbell",
		Difficulty: exercise.DifficultyBeginner,
		Primary:    []string{"quads"}, Secondary: []string{"glutes"},
	}
	curl := exercise.Exercise{
		Slug: "dumbbell-curl", Name: "Dumbbell Curl",
		Category: exercise.CategoryStrength, Equipment: "dumbbell",
		Difficulty: exercise.DifficultyBeginner, Primary: []string{"biceps"},
	}

	catalog := &stubCatalog{rows: []exercise.Exercise{squat, frontSquat, curl}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	stored, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	suggestions, err := svc.SuggestReplacements(ctx, user, stored.ID, 0, 0)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}

	for _, s := range suggestions {
		if s.Slug == "dumbbell-goblet-squat" {
			t.Error("the exercise already in the slot was offered as its own replacement")
		}
		if s.Slug == "dumbbell-curl" {
			t.Error("a curl was suggested to replace a squat; it shares no muscles")
		}
	}
	if len(suggestions) == 0 || suggestions[0].Slug != "dumbbell-front-squat" {
		t.Errorf("suggestions = %v, want the front squat first", suggestions)
	}
}

func benchPress() exercise.Exercise {
	return exercise.Exercise{
		Slug: "dumbbell-bench-press", Name: "Dumbbell Bench Press",
		Category: exercise.CategoryStrength, Equipment: "dumbbell",
		Difficulty: exercise.DifficultyBeginner,
		Primary:    []string{"chest"}, Secondary: []string{"triceps"},
	}
}

// A planned session with one exercise, so add and remove both have something
// unambiguous to act on.
func newEditablePlan(t *testing.T) (*workouts.Service, users.User, workouts.StoredPlan, *stubCatalog) {
	t.Helper()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat(), benchPress()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	stored, err := svc.CreatePlan(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return svc, user, stored, catalog
}

func TestAddingAnExerciseAppendsItAtTheDocumentedDefaults(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)
	before := len(stored.Plan.Days[0].Exercises)

	edited, err := svc.AddExercise(context.Background(), user, stored.ID, 0, "dumbbell-bench-press")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	exercises := edited.Plan.Days[0].Exercises
	if len(exercises) != before+1 {
		t.Fatalf("day has %d exercises, want %d", len(exercises), before+1)
	}

	added := exercises[len(exercises)-1]
	if added.Name != "Dumbbell Bench Press" {
		t.Errorf("appended %q", added.Name)
	}
	if added.CatalogSlug != "dumbbell-bench-press" || added.IllustrationSlug != benchPress().IllustrationSlug {
		t.Errorf("catalog references not carried: %q / %q", added.CatalogSlug, added.IllustrationSlug)
	}
	if added.Sets != plan.DefaultSets || added.Reps != plan.DefaultReps || added.RestSeconds != plan.DefaultRestSeconds {
		t.Errorf("defaults not applied: %d x %s, %ds", added.Sets, added.Reps, added.RestSeconds)
	}
	// Same append-only guarantee as a swap.
	if edited.ID == stored.ID || edited.Source != workouts.SourceEdited {
		t.Error("adding did not insert a new plan row")
	}
}

func TestRemovingAnExerciseDropsOnlyThatOne(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)
	ctx := context.Background()

	withTwo, err := svc.AddExercise(ctx, user, stored.ID, 0, "dumbbell-bench-press")
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	edited, err := svc.RemoveExercise(ctx, user, withTwo.ID, 0, 0)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	exercises := edited.Plan.Days[0].Exercises
	if len(exercises) != 1 {
		t.Fatalf("day has %d exercises, want 1", len(exercises))
	}
	if exercises[0].Name != "Dumbbell Bench Press" {
		t.Errorf("the wrong exercise survived: %q", exercises[0].Name)
	}

	// The plan the removal was made from is still readable in full.
	reread, err := svc.GetPlan(ctx, withTwo.ID, user.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if len(reread.Plan.Days[0].Exercises) != 2 {
		t.Error("the removal reached back into the plan it was made from")
	}
}

// Both new operations go through applyEdit, so both must refuse a plan that has
// been superseded rather than forking the history.
func TestAddingAndRemovingRefuseASupersededPlan(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)
	ctx := context.Background()

	if _, err := svc.AddExercise(ctx, user, stored.ID, 0, "dumbbell-bench-press"); err != nil {
		t.Fatalf("first add: %v", err)
	}

	if _, err := svc.AddExercise(ctx, user, stored.ID, 0, "dumbbell-bench-press"); !apperr.Is(err, workouts.ErrPlanSuperseded) {
		t.Errorf("stale add error = %v, want ErrPlanSuperseded", err)
	}
	if _, err := svc.RemoveExercise(ctx, user, stored.ID, 0, 0); !apperr.Is(err, workouts.ErrPlanSuperseded) {
		t.Errorf("stale remove error = %v, want ErrPlanSuperseded", err)
	}
}

// A day index from a URL must not reach a slice.
func TestEditingADayThatDoesNotExistIsRejected(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)
	ctx := context.Background()

	if _, err := svc.AddExercise(ctx, user, stored.ID, 99, "dumbbell-bench-press"); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("add to a missing day = %v, want ErrValidation", err)
	}
	if _, err := svc.RemoveExercise(ctx, user, stored.ID, 0, 99); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("remove of a missing exercise = %v, want ErrValidation", err)
	}
}

// Suggesting something the session already contains is the one suggestion that
// is certainly wrong.
func TestAddSuggestionsExcludeWhatTheDayAlreadyHas(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)

	suggestions, err := svc.SuggestForDay(context.Background(), user, stored.ID, 0)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}

	for _, s := range suggestions {
		if s.Slug == "dumbbell-goblet-squat" {
			t.Error("suggested an exercise the day already contains")
		}
	}
	if len(suggestions) == 0 {
		t.Fatal("no suggestions at all, though the catalog has an unused exercise")
	}
}

// Reordering and re-prescribing go through the same applyEdit as the rest, so
// they inherit append-only storage and the superseded guard. What is worth
// pinning here is that they reach the plan at all.
func TestMovingAnExerciseReordersTheStoredPlan(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)
	ctx := context.Background()

	withTwo, err := svc.AddExercise(ctx, user, stored.ID, 0, "dumbbell-bench-press")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if withTwo.Plan.Days[0].Exercises[0].Name != "Dumbbell Goblet Squat" {
		t.Fatalf("unexpected starting order: %q first", withTwo.Plan.Days[0].Exercises[0].Name)
	}

	edited, err := svc.MoveExercise(ctx, user, withTwo.ID, 0, 0, 1)
	if err != nil {
		t.Fatalf("move: %v", err)
	}

	exercises := edited.Plan.Days[0].Exercises
	if exercises[0].Name != "Dumbbell Bench Press" || exercises[1].Name != "Dumbbell Goblet Squat" {
		t.Errorf("order is %q then %q", exercises[0].Name, exercises[1].Name)
	}
	if edited.ID == withTwo.ID || edited.Source != workouts.SourceEdited {
		t.Error("moving did not insert a new plan row")
	}
}

func TestSettingThePrescriptionChangesTheDoseNotTheMovement(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)

	edited, err := svc.SetPrescription(context.Background(), user, stored.ID, 0, 0, 4, "6-8", 150)
	if err != nil {
		t.Fatalf("set prescription: %v", err)
	}

	got := edited.Plan.Days[0].Exercises[0]
	if got.Sets != 4 || got.Reps != "6-8" || got.RestSeconds != 150 {
		t.Errorf("prescription = %d x %s, %ds", got.Sets, got.Reps, got.RestSeconds)
	}
	if got.CatalogSlug != "dumbbell-goblet-squat" {
		t.Errorf("the movement changed: %q", got.CatalogSlug)
	}
}

// The numbers arrive from a form, so nonsense has to be refused rather than
// stored. plan.SetPrescription owns the rules; this pins that the service
// surfaces them as a validation failure.
func TestAnImpossiblePrescriptionIsRejected(t *testing.T) {
	t.Parallel()

	svc, user, stored, _ := newEditablePlan(t)
	ctx := context.Background()

	cases := []struct {
		name string
		sets int
		reps string
		rest int
	}{
		{"no sets", 0, "8-12", 90},
		{"no reps", 3, "", 90},
		{"negative rest", 3, "8-12", -1},
	}

	for _, c := range cases {
		if _, err := svc.SetPrescription(ctx, user, stored.ID, 0, 0, c.sets, c.reps, c.rest); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", c.name, err)
		}
	}

	// And nothing was written by any of them.
	plans, err := svc.ListPlans(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if len(plans) != 1 {
		t.Errorf("found %d plans, want 1 — a refused edit must not insert a row", len(plans))
	}
}

// The plans list is the reason /app/training is no longer the only way to reach
// a plan. What it must get right is one row per plan rather than one per
// version, and that row being the version being followed.
func TestListCurrentPlansReturnsOneRowPerPlanNotPerVersion(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat(), benchPress()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	first, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	second, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("second plan: %v", err)
	}

	current, err := svc.ListCurrentPlans(ctx, user.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(current) != 2 {
		t.Fatalf("listed %d plans, want 2", len(current))
	}
	// Most recently touched first, so /app/training and this page agree.
	if current[0].ID != second.ID {
		t.Errorf("first row is %v, want the newer plan %v", current[0].ID, second.ID)
	}

	// Editing the older plan makes it three rows in the table but must not make
	// it two entries in the list — and it must move to the top, because it is
	// now the most recently touched.
	edited, err := svc.SwapExercise(ctx, user, first.ID, 0, 0, "dumbbell-bench-press")
	if err != nil {
		t.Fatalf("swap: %v", err)
	}

	current, err = svc.ListCurrentPlans(ctx, user.ID, 0)
	if err != nil {
		t.Fatalf("list after edit: %v", err)
	}
	if len(current) != 2 {
		t.Fatalf("listed %d plans after an edit, want 2 — versions must not appear as plans", len(current))
	}

	// The edited plan's row has to be its newest version. A query grouping on
	// the wrong column would hand back the row as first generated, and the page
	// would show people a plan they had already changed.
	if current[0].ID != edited.ID {
		t.Errorf("first row is %v, want the edited version %v", current[0].ID, edited.ID)
	}
	if current[0].IntakeID != first.IntakeID {
		t.Error("the edited row lost the intake that identifies its plan")
	}

	// And every version is still stored; only the list collapses them.
	all, err := svc.ListPlans(ctx, user.ID, 20)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("found %d rows, want 3 — history must not have been pruned", len(all))
	}
}

func TestListCurrentPlansIsEmptyForSomeoneWithNoPlans(t *testing.T) {
	t.Parallel()

	svc, user := newServiceWithCatalog(t, &fake.Client{}, &stubCatalog{})

	current, err := svc.ListCurrentPlans(context.Background(), user.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(current) != 0 {
		t.Errorf("listed %d plans for an account with none", len(current))
	}
}

// The query filters on user_id. A plan is a record of somebody's training, and
// leaking one into another account's list would be the worst kind of bug here.
func TestListCurrentPlansNeverShowsAnotherAccountsPlans(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, owner, pool := newServiceWithCatalogAndPool(t, client, catalog)
	ctx := context.Background()

	if _, err := svc.CreatePlan(ctx, owner, dumbbellIntake()); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	stranger, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        "stranger@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Someone Else",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("create the other account: %v", err)
	}

	current, err := svc.ListCurrentPlans(ctx, stranger.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(current) != 0 {
		t.Fatalf("another account's list holds %d plans", len(current))
	}
}

// Generating a second plan must not make the first uneditable.
//
// The edit guard originally compared against the account's newest row, so any
// plan but the most recent one refused every edit — and once the plans list
// made older plans reachable, that was a page of buttons that could only fail.
// "Superseded" means this plan changed under me, not that a different plan
// exists.
func TestAnOlderPlanIsStillEditableAfterANewerOneIsGenerated(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat(), benchPress()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	older, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	if _, err = svc.CreatePlan(ctx, user, dumbbellIntake()); err != nil {
		t.Fatalf("second plan: %v", err)
	}

	edited, err := svc.SwapExercise(ctx, user, older.ID, 0, 0, "dumbbell-bench-press")
	if err != nil {
		t.Fatalf("editing the older plan: %v", err)
	}

	if edited.IntakeID != older.IntakeID {
		t.Error("the edit landed on the wrong plan")
	}
	if got := edited.Plan.Days[0].Exercises[0].Name; got != "Dumbbell Bench Press" {
		t.Errorf("the swap did not apply: %q", got)
	}
}

// The race that is worth catching still is: the same plan edited twice from a
// stale handle.
func TestTheSamePlanEditedTwiceFromAStaleHandleIsStillRefused(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat(), benchPress()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	stored, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if _, err := svc.SwapExercise(ctx, user, stored.ID, 0, 0, "dumbbell-bench-press"); err != nil {
		t.Fatalf("first swap: %v", err)
	}
	if _, err := svc.SwapExercise(ctx, user, stored.ID, 0, 0, "dumbbell-bench-press"); !apperr.Is(err, workouts.ErrPlanSuperseded) {
		t.Fatalf("second swap on the same stale handle = %v, want ErrPlanSuperseded", err)
	}
}

// And the recovery path has to send someone back to the plan they were editing.
func TestCurrentVersionOfResolvesTheNamedPlanNotTheNewestOne(t *testing.T) {
	t.Parallel()

	catalog := &stubCatalog{rows: []exercise.Exercise{gobletSquat(), benchPress()}}
	client := &fake.Client{Responses: []fake.Response{
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
		{Text: planJSON(t, planCitingCatalog("dumbbell-goblet-squat"))},
	}}

	svc, user := newServiceWithCatalog(t, client, catalog)
	ctx := context.Background()

	older, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	newer, err := svc.CreatePlan(ctx, user, dumbbellIntake())
	if err != nil {
		t.Fatalf("second plan: %v", err)
	}

	edited, err := svc.SwapExercise(ctx, user, older.ID, 0, 0, "dumbbell-bench-press")
	if err != nil {
		t.Fatalf("swap: %v", err)
	}

	// A stale handle on the older plan resolves to that plan's newest version.
	got, err := svc.CurrentVersionOf(ctx, user, older.ID)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if got.ID != edited.ID {
		t.Errorf("resolved to %v, want the older plan's newest version %v", got.ID, edited.ID)
	}
	if got.ID == newer.ID {
		t.Error("recovery sent the request to a different plan entirely")
	}
}
