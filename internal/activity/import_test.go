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

// One workout recorded by two providers — a watch run that also syncs to
// Strava, or a session timed in the app and written to Apple Health — is one
// workout. Counting it twice doubles the calories the coach reports.
func TestImportSkipsTheSameWorkoutFromAnotherSource(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	strava := importInput("strava-1")
	strava.UserID = user.ID
	if _, imported, err := svc.Import(ctx, strava); err != nil || !imported {
		t.Fatalf("strava import = %v, %v", imported, err)
	}

	// The watch's copy starts two minutes early and ends one minute late.
	watch := strava
	watch.Source = "apple_health"
	watch.ExternalID = "hk-1"
	watch.StartedAt = strava.StartedAt.Add(-2 * time.Minute)
	watch.EndedAt = strava.EndedAt.Add(time.Minute)
	existing, imported, err := svc.Import(ctx, watch)
	if err != nil {
		t.Fatalf("watch import: %v", err)
	}
	if imported {
		t.Error("the watch's copy of the Strava run was imported as a second workout")
	}
	if existing.Source != activity.SourceStrava {
		t.Errorf("returned %q, want the Strava session it duplicates", existing.Source)
	}

	sessions, err := svc.List(ctx, user.ID, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("stored %d sessions, want 1", len(sessions))
	}
}

// Back-to-back workouts are two workouts, even from different providers: a
// run logged on Strava, then a strength session from the watch that starts as
// the run ends and overlaps it by a few minutes at most.
func TestImportKeepsWorkoutsThatOnlyTouch(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	run := importInput("strava-2")
	run.UserID = user.ID
	if _, _, err := svc.Import(ctx, run); err != nil {
		t.Fatalf("run: %v", err)
	}

	lift := run
	lift.Source = "apple_health"
	lift.ExternalID = "hk-2"
	lift.ActivityCode = "strength_training"
	lift.StartedAt = run.EndedAt.Add(-5 * time.Minute)
	lift.EndedAt = lift.StartedAt.Add(45 * time.Minute)
	if _, imported, err := svc.Import(ctx, lift); err != nil || !imported {
		t.Fatalf("lift import = %v, %v; want imported", imported, err)
	}
}
