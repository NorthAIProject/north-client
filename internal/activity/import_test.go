package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func importInput(externalID string) activity.ImportInput {
	started := time.Now().Add(-90 * time.Minute)
	return activity.ImportInput{
		ActivityCode: "running_9_8kmh",
		Source:       activity.SourceStrava,
		ExternalID:   externalID,
		StartedAt:    started,
		EndedAt:      started.Add(time.Hour),
		WeightKg:     80,
	}
}

// The property the whole sync depends on: running it twice must not double
// someone's training history. Guaranteed by UNIQUE (source, external_id)
// plus ON CONFLICT DO NOTHING, so a sync can run as often as it likes.
func TestImportingTheSameActivityTwiceInsertsOnce(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	in := importInput("12345")
	in.UserID = user.ID

	session, imported, err := svc.Import(ctx, in)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if !imported {
		t.Fatal("first import reported as already present")
	}
	if session.Source != activity.SourceStrava {
		t.Errorf("Source = %q, want strava", session.Source)
	}
	if session.Status != activity.StatusCompleted {
		t.Errorf("Status = %q, want completed", session.Status)
	}

	if _, imported, err = svc.Import(ctx, in); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if imported {
		t.Error("the same activity was imported twice")
	}

	sessions, err := svc.List(ctx, user.ID, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("stored %d sessions, want 1", len(sessions))
	}
}

// An imported session is already finished, so it must not collide with the
// partial unique index that allows only one *open* session per user.
func TestImportDoesNotConflictWithAnOpenSession(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	if _, err := svc.Start(ctx, user.ID, "strength_training"); err != nil {
		t.Fatalf("start a live session: %v", err)
	}

	in := importInput("999")
	in.UserID = user.ID
	if _, imported, err := svc.Import(ctx, in); err != nil || !imported {
		t.Fatalf("import alongside an open session = %v, %v", imported, err)
	}
}

// The provider's own figure wins when it has one: a device that watched
// heart rate knows more than a MET table does.
func TestProviderCaloriesArePreferredOverTheEstimate(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	in := importInput("555")
	in.UserID = user.ID
	in.Calories = 742

	session, _, err := svc.Import(ctx, in)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if session.CaloriesBurned == nil || *session.CaloriesBurned != 742 {
		t.Errorf("CaloriesBurned = %v, want 742", session.CaloriesBurned)
	}
}

func TestWithoutProviderCaloriesTheMETEstimateIsUsed(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	in := importInput("556")
	in.UserID = user.ID

	session, _, err := svc.Import(ctx, in)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	// running_9_8kmh is 9.8 MET; one hour at 80kg.
	want := 9.8 * 80 * 1.0
	if session.CaloriesBurned == nil || *session.CaloriesBurned != want {
		t.Errorf("CaloriesBurned = %v, want %v", session.CaloriesBurned, want)
	}
}

func TestImportValidatesItsInput(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	noExternalID := importInput("")
	noExternalID.UserID = user.ID
	if _, _, err := svc.Import(ctx, noExternalID); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("import without an external id = %v, want validation error", err)
	}

	unknownCode := importInput("777")
	unknownCode.UserID = user.ID
	unknownCode.ActivityCode = "interpretive_dance"
	if _, _, err := svc.Import(ctx, unknownCode); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("import with an unknown code = %v, want validation error", err)
	}
}
