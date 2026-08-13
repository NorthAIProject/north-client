package habits_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*habits.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	return habits.NewService(habits.NewRepository(pool)), user
}

func everyDay() []time.Weekday {
	return []time.Weekday{
		time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday,
	}
}

func TestCreateAndList(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, user, habits.Input{
		Name:   "Meditate",
		Domain: "personal",
		Days:   everyDay(),
		Active: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "Meditate" || created.Domain != "personal" {
		t.Errorf("created = %+v", created)
	}
	if !created.IsDaily() {
		t.Errorf("Days = %v, want all seven", created.Days)
	}

	list, err := svc.List(ctx, user, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("listed %d habits, want 1", len(list))
	}
}

// Ticking twice is the same as ticking once: the unique constraint plus
// ON CONFLICT DO NOTHING makes a double tap harmless rather than an error.
func TestCompletingTwiceInADayIsIdempotent(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	h, err := svc.Create(ctx, user, habits.Input{Name: "Read", Days: everyDay(), Active: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err = svc.Complete(ctx, user, h.ID); err != nil {
			t.Fatalf("complete %d: %v", i, err)
		}
	}

	stats, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("got %d stats, want 1", len(stats))
	}
	if !stats[0].DoneToday {
		t.Error("DoneToday = false after completing")
	}
	if stats[0].Streak != 1 {
		t.Errorf("Streak = %d, want 1 — three taps are still one day", stats[0].Streak)
	}
}

func TestUncompleteUndoesToday(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	h, err := svc.Create(ctx, user, habits.Input{Name: "Read", Days: everyDay(), Active: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err = svc.Complete(ctx, user, h.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err = svc.Uncomplete(ctx, user, h.ID); err != nil {
		t.Fatalf("uncomplete: %v", err)
	}

	stats, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if stats[0].DoneToday {
		t.Error("DoneToday = true after uncompleting")
	}
	if stats[0].Streak != 0 {
		t.Errorf("Streak = %d, want 0", stats[0].Streak)
	}
}

// Today's stats must reflect whether the habit is even due today, so the UI
// can tell "missed" apart from "not your day".
func TestTodayDistinguishesNotDueFromNotDone(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	today := habits.LocalDate(user, time.Now())

	// One habit due only today, one due only on a different day.
	dueToday, err := svc.Create(ctx, user, habits.Input{
		Name: "Due", Days: []time.Weekday{today.Weekday()}, Active: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	notDue, err := svc.Create(ctx, user, habits.Input{
		Name: "Not due", Days: []time.Weekday{today.AddDate(0, 0, 1).Weekday()}, Active: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	stats, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatalf("today: %v", err)
	}

	byID := map[string]bool{}
	for _, s := range stats {
		byID[s.Habit.ID.String()] = s.ScheduledToday
	}
	if !byID[dueToday.ID.String()] {
		t.Error("a habit scheduled for today reported ScheduledToday = false")
	}
	if byID[notDue.ID.String()] {
		t.Error("a habit not scheduled for today reported ScheduledToday = true")
	}
}

func TestArchivedHabitsDropOutOfTheActiveList(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	h, err := svc.Create(ctx, user, habits.Input{Name: "Old habit", Days: everyDay(), Active: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err = svc.Update(ctx, user, h.ID, habits.Input{
		Name: "Old habit", Domain: "personal", Days: everyDay(), Active: false,
	}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	active, err := svc.List(ctx, user, true)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("archived habit still active: %+v", active)
	}

	// Archived rather than deleted: the history is still there.
	all, err := svc.List(ctx, user, false)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("listed %d habits including archived, want 1", len(all))
	}
}

func TestHabitsAreScopedToTheirOwner(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	h, err := svc.Create(ctx, user, habits.Input{Name: "Mine", Days: everyDay(), Active: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	stranger := user
	stranger.ID = users.User{}.ID

	if _, err := svc.Get(ctx, stranger, h.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("Get by stranger = %v, want not found", err)
	}
	if err := svc.Complete(ctx, stranger, h.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("Complete by stranger = %v, want not found", err)
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   habits.Input
	}{
		{"no name", habits.Input{Days: everyDay()}},
		{"no days", habits.Input{Name: "Something"}},
		{"unknown domain", habits.Input{Name: "Something", Domain: "wizardry", Days: everyDay()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := habits.Validate(tt.in); !apperr.Is(err, apperr.ErrValidation) {
				t.Errorf("Validate(%+v) = %v, want validation error", tt.in, err)
			}
		})
	}
}

// Storage order must never affect behaviour, and a day listed twice is still
// one day.
func TestDaysAreNormalizedOnWrite(t *testing.T) {
	t.Parallel()

	clean, err := habits.Validate(habits.Input{
		Name: "Lift",
		Days: []time.Weekday{time.Friday, time.Monday, time.Monday, time.Wednesday},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	want := []time.Weekday{time.Monday, time.Wednesday, time.Friday}
	if len(clean.Days) != len(want) {
		t.Fatalf("Days = %v, want %v", clean.Days, want)
	}
	for i := range want {
		if clean.Days[i] != want[i] {
			t.Errorf("Days[%d] = %v, want %v", i, clean.Days[i], want[i])
		}
	}
}

func TestDomainDefaultsRatherThanFailing(t *testing.T) {
	t.Parallel()

	clean, err := habits.Validate(habits.Input{Name: "Something", Days: everyDay()})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if clean.Domain != "personal" {
		t.Errorf("Domain = %q, want personal", clean.Domain)
	}
}
