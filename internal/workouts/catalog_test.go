package workouts_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
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

func gobletSquat() exercise.Exercise {
	return exercise.Exercise{
		Slug: "dumbbell-goblet-squat", Name: "Dumbbell Goblet Squat",
		Category: exercise.CategoryStrength, Equipment: "dumbbell", Difficulty: exercise.DifficultyBeginner,
		Primary: []string{"quads"}, Secondary: []string{"glutes", "abs"},
	}
}

func newServiceWithCatalog(t *testing.T, client *fake.Client, catalog workouts.Catalog) (*workouts.Service, users.User) {
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
	}), user
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
