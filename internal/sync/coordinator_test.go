package sync

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeNudges struct {
	dismissed []dismissCall
}

type dismissCall struct {
	userID     uuid.UUID
	kind       string
	dedupeKey  string
	updateText string
}

func (f *fakeNudges) DismissByKind(ctx context.Context, userID uuid.UUID, kind, dedupeKey, updateText string) error {
	f.dismissed = append(f.dismissed, dismissCall{
		userID:     userID,
		kind:       kind,
		dedupeKey:  dedupeKey,
		updateText: updateText,
	})
	return nil
}

func TestOnWorkoutCompleted(t *testing.T) {
	nudges := &fakeNudges{}
	c := NewCoordinator(nudges)
	userID := uuid.New()

	err := c.OnWorkoutCompleted(context.Background(), userID, "Upper A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(nudges.dismissed) != 1 {
		t.Fatalf("expected 1 dismiss call, got %d", len(nudges.dismissed))
	}
	call := nudges.dismissed[0]
	if call.kind != "workout_today" {
		t.Errorf("expected kind workout_today, got %s", call.kind)
	}
	today := time.Now().Format("2006-01-02")
	if call.dedupeKey != today {
		t.Errorf("expected dedupeKey %s, got %s", today, call.dedupeKey)
	}
	if call.updateText != "✅ Completed today's session: Upper A" {
		t.Errorf("unexpected update text: %s", call.updateText)
	}
}

func TestOnCheckInSaved(t *testing.T) {
	nudges := &fakeNudges{}
	c := NewCoordinator(nudges)
	userID := uuid.New()

	err := c.OnCheckInSaved(context.Background(), userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(nudges.dismissed) != 2 {
		t.Fatalf("expected 2 dismiss calls, got %d", len(nudges.dismissed))
	}
	if nudges.dismissed[0].kind != "missed_checkin" || nudges.dismissed[1].kind != "streak_at_risk" {
		t.Errorf("unexpected kinds: %+v", nudges.dismissed)
	}
}
