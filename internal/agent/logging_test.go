package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

func habitFixture(names ...string) []habits.Habit {
	list := make([]habits.Habit, 0, len(names))
	for _, name := range names {
		list = append(list, habits.Habit{ID: uuid.New(), Name: name})
	}
	return list
}

func TestMatchHabitTakesAnExactNameFirst(t *testing.T) {
	t.Parallel()

	list := habitFixture("Read", "Read 20 pages")

	got, err := matchHabit(list, "read")
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	// "Read" is also a substring of "Read 20 pages"; the exact match must win
	// rather than the name being called ambiguous.
	if got.Name != "Read" {
		t.Fatalf("matched %q, want %q", got.Name, "Read")
	}
}

func TestMatchHabitAcceptsAUniquePartialName(t *testing.T) {
	t.Parallel()

	list := habitFixture("Read 20 pages", "Cold shower")

	got, err := matchHabit(list, "cold")
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if got.Name != "Cold shower" {
		t.Fatalf("matched %q, want %q", got.Name, "Cold shower")
	}
}

// The important one: ticking off the wrong habit is invisible to the person
// and corrupts a streak, so an ambiguous name must ask rather than guess.
func TestMatchHabitRefusesAnAmbiguousName(t *testing.T) {
	t.Parallel()

	list := habitFixture("Morning walk", "Morning pages")

	_, err := matchHabit(list, "morning")
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}
	for _, want := range []string{"Morning walk", "Morning pages"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name the candidate %q: %s", want, err)
		}
	}
}

func TestMatchHabitNamesWhatExistsWhenNothingMatches(t *testing.T) {
	t.Parallel()

	_, err := matchHabit(habitFixture("Read 20 pages"), "meditate")
	if !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("want a not-found error, got %v", err)
	}
	// A dead end is worse than a signpost: the refusal has to say what the
	// person does keep, or the model cannot recover in one turn.
	if !strings.Contains(err.Error(), "Read 20 pages") {
		t.Errorf("error should list the habits they keep: %s", err)
	}
}

func TestMatchHabitSaysSoWhenThereAreNoHabits(t *testing.T) {
	t.Parallel()

	_, err := matchHabit(nil, "anything")
	if !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("want a not-found error, got %v", err)
	}
}

// The logging tools are the ones that write, so the flags a client honours are
// worth pinning here as well as in the MCP contract.
func TestLoggingCapabilitiesDeclareTheirWrites(t *testing.T) {
	t.Parallel()

	r := loggingRegistry(t)

	for _, name := range []string{"log_water", "log_sleep", "complete_habit", "record_weight", "log_food"} {
		if r.IsReadOnly(name) {
			t.Errorf("%s reports read-only; it writes", name)
		}
	}

	idempotent := map[string]bool{
		"log_water":      false,
		"log_sleep":      true,
		"complete_habit": true,
		"record_weight":  true,
		"log_food":       false,
	}
	for name, want := range idempotent {
		if got := r.IsIdempotent(name); got != want {
			t.Errorf("%s idempotent=%t, want %t", name, got, want)
		}
	}
}

func loggingRegistry(t *testing.T) *Registry {
	t.Helper()

	return Build(Services{
		Hydration:  hydration.NewService(hydration.NewRepository(nil)),
		Sleep:      sleep.NewService(sleep.NewRepository(nil)),
		Habits:     habits.NewService(habits.NewRepository(nil)),
		Biometrics: biometrics.NewService(biometrics.NewRepository(nil)),
		Users:      users.NewService(users.NewRepository(nil)),
	})
}

// A logging service registered without Users would publish a tool that fails on
// every call, so Build must not register those three at all.
func TestTimezoneBoundToolsNeedTheUserRecord(t *testing.T) {
	t.Parallel()

	r := Build(Services{
		Hydration:  hydration.NewService(hydration.NewRepository(nil)),
		Sleep:      sleep.NewService(sleep.NewRepository(nil)),
		Habits:     habits.NewService(habits.NewRepository(nil)),
		Biometrics: biometrics.NewService(biometrics.NewRepository(nil)),
	})

	published := map[string]bool{}
	for _, tool := range r.Tools() {
		published[tool.Name] = true
	}

	for _, name := range []string{"log_water", "log_sleep", "complete_habit"} {
		if published[name] {
			t.Errorf("%s was published without a users service; it would fail on every call", name)
		}
	}
	// A weight is not filed against a local date, so it needs no user record.
	if !published["record_weight"] {
		t.Error("record_weight should be published without a users service")
	}
}

// End to end against a real database: the tools have to write real rows, not
// merely decode their arguments.
func TestLoggingCapabilitiesWriteRealRows(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	sleepSvc := sleep.NewService(sleep.NewRepository(pool))
	habitSvc := habits.NewService(habits.NewRepository(pool))
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))

	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "capture@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	habit, err := habitSvc.Create(ctx, user, habits.Input{
		Name:   "Read 20 pages",
		Domain: lifedomain.Learning,
		Days:   []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday},
		Active: true,
	})
	if err != nil {
		t.Fatalf("create habit: %v", err)
	}

	if _, baseErr := biometricSvc.Record(ctx, user.ID, biometrics.Input{
		WeightKg: 80, HeightCm: 180,
		DateOfBirth: time.Now().AddDate(-30, 0, 0),
		Sex:         biometrics.SexMale,
	}); baseErr != nil {
		t.Fatalf("baseline: %v", baseErr)
	}

	r := Build(Services{
		Hydration:  hydrationSvc,
		Sleep:      sleepSvc,
		Habits:     habitSvc,
		Biometrics: biometricSvc,
		Users:      userSvc,
	})

	invoke := func(name string, args any) string {
		t.Helper()
		raw, marshalErr := json.Marshal(args)
		if marshalErr != nil {
			t.Fatalf("marshal %s args: %v", name, marshalErr)
		}
		result := r.Invoke(ctx, user.ID, ai.ToolCall{Name: name, Arguments: raw})
		if result.IsError {
			t.Fatalf("%s: %s", name, result.Content)
		}
		return result.Content
	}

	if out := invoke("log_water", map[string]any{"amount_ml": 500}); !strings.Contains(out, "500") {
		t.Errorf("log_water said %q", out)
	}
	if day, dayErr := hydrationSvc.Today(ctx, user); dayErr != nil || day.TotalML != 500 {
		t.Fatalf("hydration total = %d (err %v), want 500", day.TotalML, dayErr)
	}

	invoke("log_sleep", map[string]any{"minutes": 390, "quality": 4, "notes": ""})
	night, ok, err := sleepSvc.Today(ctx, user)
	if err != nil || !ok {
		t.Fatalf("sleep today: ok=%t err=%v", ok, err)
	}
	if night.DurationMinutes != 390 {
		t.Errorf("sleep = %d minutes, want 390", night.DurationMinutes)
	}

	invoke("complete_habit", map[string]any{"name": "read 20 pages"})
	stats, err := habitSvc.Today(ctx, user)
	if err != nil {
		t.Fatalf("habits today: %v", err)
	}
	var done bool
	for _, s := range stats {
		if s.Habit.ID == habit.ID && s.DoneToday {
			done = true
		}
	}
	if !done {
		t.Error("complete_habit did not mark the habit done today")
	}

	invoke("record_weight", map[string]any{"kg": 78.4})
	current, err := biometricSvc.Current(ctx, user.ID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.WeightKg != 78.4 {
		t.Errorf("weight = %v, want 78.4", current.WeightKg)
	}
	// The overlay, not a fresh measurement: height came from the baseline.
	if current.HeightCm != 180 {
		t.Errorf("height = %v, want the baseline 180", current.HeightCm)
	}
}
