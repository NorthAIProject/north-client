package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeBiometrics struct {
	bio biometrics.Biometric
	err error
}

func (f fakeBiometrics) Current(context.Context, uuid.UUID) (biometrics.Biometric, error) {
	return f.bio, f.err
}

func newService(t *testing.T, lookup activity.BiometricsLookup) (*activity.Service, users.User) {
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

	return activity.NewService(activity.NewRepository(pool), lookup), user
}

func withWeight(kg float64) activity.BiometricsLookup {
	return fakeBiometrics{bio: biometrics.Biometric{WeightKg: kg, HeightCm: 180, Sex: biometrics.SexMale}}
}

func TestElapsedWhileActive(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	s := activity.Session{StartedAt: start, Status: activity.StatusActive}

	got := s.Elapsed(start.Add(30 * time.Minute))
	if got != 30*time.Minute {
		t.Fatalf("elapsed = %v, want 30m", got)
	}
}

func TestElapsedFreezesWhilePaused(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	pausedAt := start.Add(20 * time.Minute)
	s := activity.Session{StartedAt: start, Status: activity.StatusPaused, PausedAt: &pausedAt}

	// Asking "as of" an hour later should not count time after the pause.
	got := s.Elapsed(start.Add(time.Hour))
	if got != 20*time.Minute {
		t.Fatalf("elapsed = %v, want 20m (frozen at pause)", got)
	}
}

func TestElapsedExcludesTotalPausedSeconds(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	s := activity.Session{StartedAt: start, Status: activity.StatusActive, TotalPausedSeconds: 600}

	// 40 minutes of wall-clock, 10 minutes of which was paused earlier.
	got := s.Elapsed(start.Add(40 * time.Minute))
	if got != 30*time.Minute {
		t.Fatalf("elapsed = %v, want 30m (40m wall clock minus 10m paused)", got)
	}
}

func TestElapsedFreezesAtEndedAt(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	ended := start.Add(45 * time.Minute)
	s := activity.Session{StartedAt: start, Status: activity.StatusCompleted, EndedAt: &ended}

	got := s.Elapsed(start.Add(3 * time.Hour))
	if got != 45*time.Minute {
		t.Fatalf("elapsed = %v, want 45m (frozen at ended_at)", got)
	}
}

func TestStartRequiresBiometrics(t *testing.T) {
	svc, user := newService(t, fakeBiometrics{err: apperr.ErrNotFound})

	if _, err := svc.Start(context.Background(), user.ID, "walking_moderate"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestStartRejectsUnknownActivity(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	if _, err := svc.Start(context.Background(), user.ID, "teleporting"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestLifecycleStartPauseResumeStop(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	started, err := svc.Start(ctx, user.ID, "running_9_8kmh")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Status != activity.StatusActive {
		t.Fatalf("status = %q, want active", started.Status)
	}
	if started.WeightKgSnapshot != 80 {
		t.Fatalf("weight snapshot = %v, want 80", started.WeightKgSnapshot)
	}

	active, ok, err := svc.Active(ctx, user.ID)
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if !ok || active.ID != started.ID {
		t.Fatal("the started session should be the active one")
	}

	paused, err := svc.Pause(ctx, started.ID, user.ID)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if paused.Status != activity.StatusPaused {
		t.Fatalf("status = %q, want paused", paused.Status)
	}

	resumed, err := svc.Resume(ctx, started.ID, user.ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Status != activity.StatusActive {
		t.Fatalf("status = %q, want active", resumed.Status)
	}

	stopped, err := svc.Stop(ctx, started.ID, user.ID)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stopped.Status != activity.StatusCompleted {
		t.Fatalf("status = %q, want completed", stopped.Status)
	}
	if stopped.CaloriesBurned == nil {
		t.Fatal("a completed session should have a calorie figure")
	}
	if *stopped.CaloriesBurned < 0 {
		t.Fatalf("calories burned = %v, want >= 0", *stopped.CaloriesBurned)
	}

	if _, ok, err := svc.Active(ctx, user.ID); err != nil {
		t.Fatalf("active: %v", err)
	} else if ok {
		t.Fatal("no session should be open after Stop")
	}
}

func TestSecondStartConflictsWhileOneIsOpen(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	if _, err := svc.Start(ctx, user.ID, "walking_moderate"); err != nil {
		t.Fatalf("first start: %v", err)
	}

	if _, err := svc.Start(ctx, user.ID, "running_9_8kmh"); !apperr.Is(err, apperr.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCancelReleasesTheOpenSessionSlot(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	started, err := svc.Start(ctx, user.ID, "walking_moderate")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := svc.Cancel(ctx, started.ID, user.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if _, ok, err := svc.Active(ctx, user.ID); err != nil {
		t.Fatalf("active: %v", err)
	} else if ok {
		t.Fatal("a cancelled session should no longer be open")
	}

	// The slot should be free again.
	if _, err := svc.Start(ctx, user.ID, "running_9_8kmh"); err != nil {
		t.Fatalf("start after cancel: %v", err)
	}
}

func TestTotalCaloriesSinceSumsCompletedSessions(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	started, err := svc.Start(ctx, user.ID, "running_9_8kmh")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err = svc.Stop(ctx, started.ID, user.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}

	total, err := svc.TotalCaloriesSince(ctx, user.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("total calories: %v", err)
	}
	if total < 0 {
		t.Fatalf("total = %v, want >= 0", total)
	}
}
