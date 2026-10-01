package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/nudges"
)

// fakeNudges records each resolve and renders its text against a stored
// nudge, the way the real service does.
type fakeNudges struct {
	title, body string
	err         error
	kinds       []string
	texts       []string
}

func (f *fakeNudges) ResolveToday(_ context.Context, _ uuid.UUID, kind string, text func(title, body string) string) error {
	f.kinds = append(f.kinds, kind)
	f.texts = append(f.texts, text(f.title, f.body))
	return f.err
}

func TestOnWorkoutCompletedNamesThePlanDay(t *testing.T) {
	fake := &fakeNudges{title: "Start today's session", body: "Upper A"}
	c := NewCoordinator(fake)

	if err := c.OnWorkoutCompleted(context.Background(), uuid.New(), "strength"); err != nil {
		t.Fatal(err)
	}

	if len(fake.kinds) != 1 || fake.kinds[0] != nudges.KindWorkoutToday {
		t.Fatalf("kinds = %v", fake.kinds)
	}
	if fake.texts[0] != "✅ Completed today's session: Upper A" {
		t.Errorf("text = %q", fake.texts[0])
	}
}

func TestOnWorkoutCompletedFallsBackToTheSessionTitle(t *testing.T) {
	fake := &fakeNudges{title: "Start today's session"}
	c := NewCoordinator(fake)

	if err := c.OnWorkoutCompleted(context.Background(), uuid.New(), "strength"); err != nil {
		t.Fatal(err)
	}
	if fake.texts[0] != "✅ Completed today's session: strength" {
		t.Errorf("text = %q", fake.texts[0])
	}
}

func TestOnCheckInSaved(t *testing.T) {
	fake := &fakeNudges{}
	c := NewCoordinator(fake)

	if err := c.OnCheckInSaved(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}

	if len(fake.kinds) != 2 || fake.kinds[0] != nudges.KindMissedCheckIn || fake.kinds[1] != nudges.KindStreakAtRisk {
		t.Fatalf("kinds = %v", fake.kinds)
	}
	if fake.texts[0] != "✅ Checked in today" {
		t.Errorf("text = %q", fake.texts[0])
	}
}

func TestOnMealLogged(t *testing.T) {
	fake := &fakeNudges{title: "log lunch"}
	c := NewCoordinator(fake)

	if err := c.OnMealLogged(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}

	if len(fake.kinds) != 1 || fake.kinds[0] != nudges.KindMealReminder {
		t.Fatalf("kinds = %v", fake.kinds)
	}
	if fake.texts[0] != "✅ Done: log lunch" {
		t.Errorf("text = %q", fake.texts[0])
	}
}

func TestResolveFailureIsReturned(t *testing.T) {
	fake := &fakeNudges{err: errors.New("database down")}
	c := NewCoordinator(fake)

	if err := c.OnWorkoutCompleted(context.Background(), uuid.New(), ""); err == nil {
		t.Fatal("want the error back, got nil")
	}
}
