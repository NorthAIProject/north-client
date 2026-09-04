package capture_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"

	"github.com/jackc/pgx/v5/pgxpool"
)

// stubParser stands in for the model. The service is about resolving and
// writing; what a sentence means is parse.go's problem and is tested there.
type stubParser struct {
	draft capture.Draft
	err   error

	sawHabits []habits.Habit
}

func (s *stubParser) Parse(_ context.Context, _ users.User, _ string, known []habits.Habit) (capture.Draft, error) {
	s.sawHabits = known
	return s.draft, s.err
}

type fixture struct {
	svc    *capture.Service
	parser *stubParser
	user   users.User
	pool   *pgxpool.Pool

	hydration  *hydration.Service
	sleep      *sleep.Service
	habits     *habits.Service
	biometrics *biometrics.Service
	checkIns   *checkins.Service
	foodLog    *meals.FoodLogService
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	// Not UTC on purpose: every log here is filed against a local date, and a
	// UTC-only test would pass with the timezone dropped entirely.
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "capture@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Pacific/Auckland",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	mealRepo := meals.NewRepository(pool)
	f := &fixture{
		parser:     &stubParser{},
		user:       user,
		pool:       pool,
		hydration:  hydration.NewService(hydration.NewRepository(pool)),
		sleep:      sleep.NewService(sleep.NewRepository(pool)),
		habits:     habits.NewService(habits.NewRepository(pool)),
		biometrics: biometrics.NewService(biometrics.NewRepository(pool)),
		checkIns:   checkins.NewService(checkins.NewRepository(pool), nil),
		foodLog:    meals.NewFoodLogService(mealRepo),
	}

	f.svc = capture.NewService(capture.Options{
		Parser:      f.parser,
		Hydration:   f.hydration,
		Sleep:       f.sleep,
		Habits:      f.habits,
		Biometrics:  f.biometrics,
		FoodLog:     f.foodLog,
		Ingredients: meals.NewIngredientService(mealRepo),
		CheckIns:    f.checkIns,
	})
	return f
}

func (f *fixture) addHabit(t *testing.T, name string) habits.Habit {
	t.Helper()
	h, err := f.habits.Create(context.Background(), f.user, habits.Input{
		Name:   name,
		Domain: lifedomain.Personal,
		Days: []time.Weekday{
			time.Monday, time.Tuesday, time.Wednesday, time.Thursday,
			time.Friday, time.Saturday, time.Sunday,
		},
		Active: true,
	})
	if err != nil {
		t.Fatalf("create habit %q: %v", name, err)
	}
	return h
}

func water(ml int) capture.Item {
	return capture.Item{Kind: capture.KindWater, Source: "water", Water: &capture.Water{AmountML: ml}}
}

func TestCommitWritesEveryKind(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	habit := f.addHabit(t, "Read 20 pages")
	if _, err := f.biometrics.Record(ctx, f.user.ID, biometrics.Input{
		WeightKg: 80, HeightCm: 180,
		DateOfBirth: time.Now().AddDate(-30, 0, 0),
		Sex:         biometrics.SexMale,
	}); err != nil {
		t.Fatalf("baseline: %v", err)
	}

	receipt, err := f.svc.Commit(ctx, f.user, []capture.Item{
		water(500),
		{Kind: capture.KindSleep, Source: "6h", Sleep: &capture.Sleep{Minutes: 360, Quality: 4}},
		{Kind: capture.KindHabit, Source: "read", Habit: &capture.Habit{Name: habit.Name, ID: habit.ID}},
		{Kind: capture.KindWeight, Source: "78kg", Weight: &capture.Weight{KG: 78}},
		{Kind: capture.KindCheckIn, Source: "mood 4", CheckIn: &capture.CheckIn{Mood: 4, Energy: 3}},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if receipt.Written() != 5 || receipt.Failed() != 0 {
		t.Fatalf("written=%d failed=%d, want 5 and 0: %+v", receipt.Written(), receipt.Failed(), receipt.Outcomes)
	}

	day, err := f.hydration.Today(ctx, f.user)
	if err != nil || day.TotalML != 500 {
		t.Errorf("hydration = %d (err %v), want 500", day.TotalML, err)
	}
	night, ok, err := f.sleep.Today(ctx, f.user)
	if err != nil || !ok || night.DurationMinutes != 360 {
		t.Errorf("sleep = %+v ok=%t err=%v", night, ok, err)
	}
	current, err := f.biometrics.Current(ctx, f.user.ID)
	if err != nil || current.WeightKg != 78 {
		t.Errorf("weight = %v (err %v), want 78", current.WeightKg, err)
	}
	entry, err := f.checkIns.Today(ctx, f.user)
	if err != nil || entry.Mood != 4 || entry.Energy != 3 {
		t.Errorf("check-in = %+v, err %v", entry, err)
	}
	stats, err := f.habits.Today(ctx, f.user)
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
		t.Error("the habit was not marked done")
	}
}

// The guarantee that makes partial failure honest: what succeeded is really in
// the database, and the receipt says which item did not.
func TestCommitKeepsWhatSucceededWhenOneItemFails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// No biometrics baseline, so the weight cannot be written.
	receipt, err := f.svc.Commit(ctx, f.user, []capture.Item{
		water(500),
		{Kind: capture.KindWeight, Source: "78kg", Weight: &capture.Weight{KG: 78}},
		{Kind: capture.KindCheckIn, Source: "mood 4", CheckIn: &capture.CheckIn{Mood: 4, Energy: 3}},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	if receipt.Written() != 2 || receipt.Failed() != 1 {
		t.Fatalf("written=%d failed=%d, want 2 and 1", receipt.Written(), receipt.Failed())
	}

	day, err := f.hydration.Today(ctx, f.user)
	if err != nil || day.TotalML != 500 {
		t.Errorf("the water was rolled back: %d ml (err %v)", day.TotalML, err)
	}
	if _, err := f.checkIns.Today(ctx, f.user); err != nil {
		t.Errorf("the check-in was rolled back: %v", err)
	}

	var failure string
	for _, o := range receipt.Outcomes {
		if o.Error != "" {
			failure = o.Error
		}
	}
	if failure == "" {
		t.Fatal("the receipt does not say which item failed")
	}
	// It has to be actionable, not "something went wrong".
	if !strings.Contains(strings.ToLower(failure), "height") {
		t.Errorf("failure should say what to do: %q", failure)
	}
}

func TestCommitAppliesInAFixedOrder(t *testing.T) {
	f := newFixture(t)

	receipt, err := f.svc.Commit(context.Background(), f.user, []capture.Item{
		{Kind: capture.KindCheckIn, Source: "mood", CheckIn: &capture.CheckIn{Mood: 4, Energy: 3}},
		water(250),
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if receipt.Outcomes[0].Item.Kind != capture.KindWater {
		t.Fatalf("first outcome is %s, want water regardless of input order", receipt.Outcomes[0].Item.Kind)
	}
}

// An unresolved item is shown, never written, and never invents a row.
func TestCommitSkipsUnresolvedItems(t *testing.T) {
	f := newFixture(t)

	receipt, err := f.svc.Commit(context.Background(), f.user, []capture.Item{
		water(250),
		{
			Kind:    capture.KindHabit,
			Source:  "meditated",
			Habit:   &capture.Habit{Name: "Meditate"},
			Problem: "You are not keeping a habit called \"Meditate\".",
		},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(receipt.Skipped) != 1 {
		t.Fatalf("skipped = %d, want 1", len(receipt.Skipped))
	}
	if receipt.Written() != 1 {
		t.Fatalf("written = %d, want 1", receipt.Written())
	}
}

func TestParseResolvesHabitNames(t *testing.T) {
	f := newFixture(t)
	habit := f.addHabit(t, "Cold shower")

	f.parser.draft = capture.Draft{Items: []capture.Item{
		{Kind: capture.KindHabit, Source: "cold shower", Habit: &capture.Habit{Name: "cold"}},
	}}

	draft, err := f.svc.Parse(context.Background(), f.user, "cold shower")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := draft.Items[0]
	if got.Habit.ID != habit.ID {
		t.Fatalf("habit id = %v, want %v", got.Habit.ID, habit.ID)
	}
	// The stored name, not the person's shorthand.
	if got.Habit.Name != "Cold shower" {
		t.Fatalf("habit name = %q, want %q", got.Habit.Name, "Cold shower")
	}
	if got.Problem != "" {
		t.Fatalf("resolved item carries a problem: %q", got.Problem)
	}
}

func TestParseExplainsAnUnknownHabitRatherThanFailing(t *testing.T) {
	f := newFixture(t)
	f.addHabit(t, "Cold shower")

	f.parser.draft = capture.Draft{Items: []capture.Item{
		water(500),
		{Kind: capture.KindHabit, Source: "meditated", Habit: &capture.Habit{Name: "Meditate"}},
	}}

	draft, err := f.svc.Parse(context.Background(), f.user, "2L water, meditated")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if draft.Items[1].Problem == "" {
		t.Fatal("an unknown habit should carry a problem")
	}
	if !draft.AnyWritable() {
		t.Fatal("the rest of the sentence should still be writable")
	}
}

func TestParseAmbiguousHabitIsAQuestionNotAGuess(t *testing.T) {
	f := newFixture(t)
	f.addHabit(t, "Morning walk")
	f.addHabit(t, "Morning pages")

	f.parser.draft = capture.Draft{Items: []capture.Item{
		{Kind: capture.KindHabit, Source: "morning", Habit: &capture.Habit{Name: "morning"}},
	}}

	draft, err := f.svc.Parse(context.Background(), f.user, "did my morning thing")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := draft.Items[0]
	if got.Habit.ID != (habits.Habit{}).ID {
		t.Fatal("an ambiguous name must not resolve")
	}
	for _, want := range []string{"Morning walk", "Morning pages"} {
		if !strings.Contains(got.Problem, want) {
			t.Errorf("problem should name %q: %s", want, got.Problem)
		}
	}
}

// The parser is told which habits exist; without that it cannot name one.
func TestParseGroundsTheModelInTheirHabits(t *testing.T) {
	f := newFixture(t)
	f.addHabit(t, "Cold shower")

	if _, err := f.svc.Parse(context.Background(), f.user, "anything"); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.parser.sawHabits) != 1 || f.parser.sawHabits[0].Name != "Cold shower" {
		t.Fatalf("parser saw %+v, want the one habit", f.parser.sawHabits)
	}
}
