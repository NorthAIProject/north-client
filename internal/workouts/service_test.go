package workouts_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

func newService(t *testing.T, client *fake.Client) (*workouts.Service, users.User) {
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

	svc := workouts.NewService(workouts.Options{
		Repository: workouts.NewRepository(pool),
		Runner:     runtime,
		Model:      "test-model",
	})

	return svc, user
}

func dumbbellIntake() workouts.Intake {
	return workouts.Intake{
		Goal:           "general strength",
		Experience:     "beginner",
		DaysPerWeek:    3,
		SessionMinutes: 45,
		Equipment:      []string{"dumbbell"},
	}
}

func planJSON(t *testing.T, plan workouts.Plan) string {
	t.Helper()
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("encode plan: %v", err)
	}
	return string(body)
}

func goodPlan() workouts.Plan {
	return workouts.Plan{
		Name: "Dumbbell foundations", Rationale: "Three full-body days suit a beginner.", WeeksTotal: 8,
		Days: []workouts.PlanDay{
			{Weekday: "Monday", Focus: "full body", Exercises: []workouts.Exercise{
				{Name: "Dumbbell Goblet Squat", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbell"},
			}},
			{Weekday: "Wednesday", Focus: "full body", Exercises: []workouts.Exercise{
				{Name: "Push-up", Sets: 3, Reps: "AMRAP", RestSeconds: 90, Equipment: "none"},
			}},
			{Weekday: "Friday", Focus: "full body", Exercises: []workouts.Exercise{
				{Name: "Dumbbell Row", Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: "dumbbell"},
			}},
		},
	}
}

// A plan that is perfectly shaped and completely unusable: it programmes a
// barbell for someone who owns dumbbells, and four days for someone with three.
func badPlan() workouts.Plan {
	p := goodPlan()
	p.Days[0].Exercises[0] = workouts.Exercise{
		Name: "Barbell Back Squat", Sets: 5, Reps: "5", RestSeconds: 180, Equipment: "barbell",
	}
	p.Days = append(p.Days, workouts.PlanDay{
		Weekday: "Saturday", Focus: "extra", Exercises: []workouts.Exercise{
			{Name: "Push-up", Sets: 3, Reps: "10", RestSeconds: 60, Equipment: "none"},
		},
	})
	return p
}

func TestCreatePlanStoresAConformingPlan(t *testing.T) {
	client := fake.Text("")
	client.Responses = []fake.Response{{Text: ""}}

	svc, user := newService(t, client)

	// Scripted through the handler so the request the service builds is visible.
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		return fake.Response{Text: planJSON(t, goodPlan())}, nil
	}

	stored, err := svc.CreatePlan(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if stored.Plan.Name != "Dumbbell foundations" {
		t.Fatalf("plan name = %q", stored.Plan.Name)
	}
	if len(stored.Plan.Days) != 3 {
		t.Fatalf("stored plan has %d days", len(stored.Plan.Days))
	}
	if stored.Model != "test-model" || stored.Provider != "fake" {
		t.Errorf("plan is missing provenance: %s / %s", stored.Model, stored.Provider)
	}
	if stored.IntakeID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Error("the plan is not linked to the intake it was generated from")
	}
}

// NOR-8: muscle groups round-trip through the real Postgres jsonb column, not
// just Go's own json.Marshal/Unmarshal — CreatePlan encodes, Postgres stores,
// and the RETURNING row is what the assertion below reads back.
func TestCreatePlanPersistsMuscleGroupsThroughStorage(t *testing.T) {
	plan := goodPlan()
	plan.Days[0].Exercises[0].Primary = []string{"quads", "glutes"}
	plan.Days[0].Exercises[0].Secondary = []string{"hamstrings"}
	plan.Days[0].Exercises[0].Stabilizers = []string{"abs"}

	client := &fake.Client{}
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		return fake.Response{Text: planJSON(t, plan)}, nil
	}

	svc, user := newService(t, client)

	stored, err := svc.CreatePlan(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	ex := stored.Plan.Days[0].Exercises[0]
	if got, want := ex.Primary, []string{"quads", "glutes"}; !slicesEqual(got, want) {
		t.Errorf("Primary = %v, want %v", got, want)
	}
	if got, want := ex.Secondary, []string{"hamstrings"}; !slicesEqual(got, want) {
		t.Errorf("Secondary = %v, want %v", got, want)
	}
	if got, want := ex.Stabilizers, []string{"abs"}; !slicesEqual(got, want) {
		t.Errorf("Stabilizers = %v, want %v", got, want)
	}

	if !strings.Contains(stored.Plan.Summary(), "(emphasis: quads, glutes)") {
		t.Errorf("Summary() lost the muscle emphasis after storage: %q", stored.Plan.Summary())
	}
}

func slicesEqual(a, b []string) bool {
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

// The heart of WP4: a model that breaks the constraints gets told exactly what
// it broke, and the corrected plan is what reaches the user.
func TestBadPlanIsRejectedAndRetriedWithTheViolations(t *testing.T) {
	var attempts int
	client := &fake.Client{}
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		attempts++
		if attempts == 1 {
			return fake.Response{Text: planJSON(t, badPlan())}, nil
		}
		return fake.Response{Text: planJSON(t, goodPlan())}, nil
	}

	svc, user := newService(t, client)

	stored, err := svc.CreatePlan(context.Background(), user, dumbbellIntake())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	if attempts != 2 {
		t.Fatalf("expected exactly one retry, got %d attempts", attempts)
	}
	if len(stored.Plan.Days) != 3 {
		t.Fatalf("the stored plan is the bad one: %d days", len(stored.Plan.Days))
	}
	for _, d := range stored.Plan.Days {
		for _, e := range d.Exercises {
			if strings.Contains(strings.ToLower(e.Name), "barbell") {
				t.Fatalf("the stored plan still uses a barbell: %q", e.Name)
			}
		}
	}

	// The retry has to name the violations, or it is just asking again.
	retry := client.Calls()[1]
	last := retry.Messages[len(retry.Messages)-1].Text()

	for _, want := range []string{"barbell", "4 training days"} {
		if !strings.Contains(strings.ToLower(last), strings.ToLower(want)) {
			t.Errorf("the retry did not tell the model about %q:\n%s", want, last)
		}
	}
}

// A model that cannot satisfy the constraints must fail loudly. Storing a plan
// the person cannot follow would be worse than returning nothing: they would
// try to follow it.
func TestAPlanThatNeverConformsIsNeverStored(t *testing.T) {
	client := &fake.Client{}
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		return fake.Response{Text: planJSON(t, badPlan())}, nil
	}

	svc, user := newService(t, client)

	if _, err := svc.CreatePlan(context.Background(), user, dumbbellIntake()); err == nil {
		t.Fatal("a persistently invalid plan must not be stored")
	}

	if _, err := svc.LatestPlan(context.Background(), user.ID); err == nil {
		t.Fatal("no plan should exist after a failed generation")
	}

	// The intake survives, so the form is pre-filled rather than asking the
	// same questions again.
	if _, err := svc.LatestIntake(context.Background(), user.ID); err != nil {
		t.Errorf("the intake should have been kept: %v", err)
	}
}

func TestMalformedJSONIsRetried(t *testing.T) {
	var attempts int
	client := &fake.Client{}
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		attempts++
		if attempts == 1 {
			return fake.Response{Text: "Here is your plan! It has three days."}, nil
		}
		return fake.Response{Text: planJSON(t, goodPlan())}, nil
	}

	svc, user := newService(t, client)

	if _, err := svc.CreatePlan(context.Background(), user, dumbbellIntake()); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("prose instead of JSON should have been retried, got %d attempts", attempts)
	}
}

func TestGenerationIsSchemaConstrainedAndLowTemperature(t *testing.T) {
	client := &fake.Client{}
	client.Handler = func(_ context.Context, _ ai.Request) (fake.Response, error) {
		return fake.Response{Text: planJSON(t, goodPlan())}, nil
	}

	svc, user := newService(t, client)

	if _, err := svc.CreatePlan(context.Background(), user, dumbbellIntake()); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	req := client.Calls()[0]

	// Without the schema this is prose generation and the whole feature is a
	// guess at parsing.
	if req.ResponseSchema == nil {
		t.Fatal("the plan request was not schema-constrained")
	}
	if req.Temperature == nil || *req.Temperature > 0.4 {
		t.Errorf("plan generation should run at low temperature, got %v", req.Temperature)
	}

	// The constraints must reach the model in the user turn, where they carry
	// the most weight.
	prompt := req.Messages[0].Text()
	for _, want := range []string{"exactly 3 days", "dumbbell", "45 minutes"} {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(want)) {
			t.Errorf("the request did not state %q:\n%s", want, prompt)
		}
	}
}

func TestIntakeValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*workouts.Intake)
	}{
		{"no goal", func(in *workouts.Intake) { in.Goal = "" }},
		{"zero days", func(in *workouts.Intake) { in.DaysPerWeek = 0 }},
		{"eight days", func(in *workouts.Intake) { in.DaysPerWeek = 8 }},
		{"impossible session", func(in *workouts.Intake) { in.SessionMinutes = 5 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := dumbbellIntake()
			tt.mutate(&in)

			if err := workouts.ValidateIntake(in); err == nil {
				t.Fatalf("%s should be rejected", tt.name)
			}
		})
	}
}
