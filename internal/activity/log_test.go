package activity_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestLogWritesAFinishedRunWithItsDistance(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	started := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	session, err := svc.Log(ctx, user.ID, activity.LogInput{
		ActivityCode: "running_9_8kmh",
		StartedAt:    started,
		Duration:     30 * time.Minute,
		DistanceM:    5000,
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	if session.Status != activity.StatusCompleted || session.Source != activity.SourceManual {
		t.Errorf("status/source = %q/%q, want completed/manual", session.Status, session.Source)
	}
	if !session.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", session.StartedAt, started)
	}
	if session.EndedAt == nil || !session.EndedAt.Equal(started.Add(30*time.Minute)) {
		t.Errorf("EndedAt = %v, want %v", session.EndedAt, started.Add(30*time.Minute))
	}
	if session.DistanceM == nil || *session.DistanceM != 5000 {
		t.Errorf("DistanceM = %v, want 5000", session.DistanceM)
	}

	// MET 9.8 * 80 kg * 0.5 h
	if session.CaloriesBurned == nil || math.Abs(*session.CaloriesBurned-392) > 0.01 {
		t.Errorf("CaloriesBurned = %v, want 392", session.CaloriesBurned)
	}
	if session.Elapsed(time.Now()) != 30*time.Minute {
		t.Errorf("Elapsed = %v, want 30m", session.Elapsed(time.Now()))
	}
}

func TestLogDefaultsToHavingJustFinished(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	before := time.Now()
	session, err := svc.Log(context.Background(), user.ID, activity.LogInput{
		ActivityCode: "yoga",
		Duration:     45 * time.Minute,
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	if session.EndedAt == nil || session.EndedAt.Before(before) || session.EndedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("EndedAt = %v, want about now", session.EndedAt)
	}
	if session.StartedAt.Sub(before) > time.Second || before.Sub(session.StartedAt) > 45*time.Minute+time.Second {
		t.Errorf("StartedAt = %v, want 45 minutes before now", session.StartedAt)
	}
	if session.DistanceM != nil {
		t.Errorf("DistanceM = %v, want nil when not recorded", *session.DistanceM)
	}
}

func TestLogDoesNotConflictWithAnOpenSession(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	if _, err := svc.Start(ctx, user.ID, "cycling_moderate"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.Log(ctx, user.ID, activity.LogInput{ActivityCode: "running_8kmh", Duration: 20 * time.Minute}); err != nil {
		t.Fatalf("log next to an open session: %v", err)
	}
}

func TestLogRequiresBiometrics(t *testing.T) {
	svc, user := newService(t, fakeBiometrics{err: apperr.ErrNotFound})

	_, err := svc.Log(context.Background(), user.ID, activity.LogInput{ActivityCode: "running_8kmh", Duration: 20 * time.Minute})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("want a validation error without biometrics, got %v", err)
	}
}

func TestLogValidatesItsInput(t *testing.T) {
	svc, user := newService(t, withWeight(80))
	ctx := context.Background()

	cases := map[string]activity.LogInput{
		"unknown activity":  {ActivityCode: "levitation", Duration: 20 * time.Minute},
		"no duration":       {ActivityCode: "running_8kmh"},
		"a day and more":    {ActivityCode: "running_8kmh", Duration: 25 * time.Hour},
		"negative distance": {ActivityCode: "running_8kmh", Duration: 20 * time.Minute, DistanceM: -1},
		"absurd distance":   {ActivityCode: "running_8kmh", Duration: 20 * time.Minute, DistanceM: 600_000},
		"in the future":     {ActivityCode: "running_8kmh", Duration: 20 * time.Minute, StartedAt: time.Now().Add(time.Hour)},
	}
	for name, in := range cases {
		if _, err := svc.Log(ctx, user.ID, in); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: want a validation error, got %v", name, err)
		}
	}
}
