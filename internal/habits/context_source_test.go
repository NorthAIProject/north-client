package habits_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/habits"
)

// The whole point of the slice reaching the coach: a habit's record has to
// arrive as prose it can reason about, not as a structure.
func TestContextSourceReportsStreakAndAdherence(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	h, err := svc.Create(ctx, user, habits.Input{
		Name:   "Meditate",
		Domain: "personal",
		Days:   everyDay(),
		Active: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Complete(ctx, user, h.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	var into coach.Context
	source := habits.NewContextSource(svc)

	if err := source.Collect(ctx, coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(into.Habits) != 1 {
		t.Fatalf("Habits = %v, want one entry", into.Habits)
	}
	for _, want := range []string{"Meditate", "personal", "every day", "1 day streak"} {
		if !strings.Contains(into.Habits[0], want) {
			t.Errorf("summary %q missing %q", into.Habits[0], want)
		}
	}
}

// A habit due today and not yet kept should say so: it is the one thing the
// coach can actually prompt about.
func TestContextSourceFlagsAHabitStillDueToday(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	today := habits.LocalDate(user, time.Now())
	if _, err := svc.Create(ctx, user, habits.Input{
		Name: "Walk", Domain: "health", Days: []time.Weekday{today.Weekday()}, Active: true,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	var into coach.Context
	if err := habits.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(into.Habits) != 1 || !strings.Contains(into.Habits[0], "due today") {
		t.Errorf("Habits = %v, want a due-today note", into.Habits)
	}
}

func TestContextSourceIsQuietWhenThereAreNoHabits(t *testing.T) {
	svc, user := newService(t)

	var into coach.Context
	if err := habits.NewContextSource(svc).Collect(context.Background(), coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	// Render() supplies the "none set up yet" label, so the source adds
	// nothing rather than inventing an empty-state string of its own.
	if len(into.Habits) != 0 {
		t.Errorf("Habits = %v, want empty", into.Habits)
	}
}
