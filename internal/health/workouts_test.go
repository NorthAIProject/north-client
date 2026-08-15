package health_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newServiceWithWorkouts(t *testing.T) (*health.Service, *activity.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "workouts@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)

	svc := health.NewService(health.NewRepository(pool))
	svc.WithWorkouts(activitySvc, biometricSvc)

	return svc, activitySvc, user
}

// The point of dropping the source CHECK: a provider that is not Strava can now
// record a finished session at all.
func TestAWorkoutFromAPhoneIsRecordedAsASession(t *testing.T) {
	svc, activitySvc, user := newServiceWithWorkouts(t)
	ctx := context.Background()

	result, err := svc.IngestWorkouts(ctx, user.ID, "apple_health", []health.Workout{{
		ActivityCode: "running_fast",
		ExternalID:   "hk-workout-1",
		StartedAt:    at("2026-08-15T07:00:00Z"),
		EndedAt:      at("2026-08-15T07:42:00Z"),
		Calories:     410,
	}})
	if err != nil {
		t.Fatalf("ingest workouts: %v", err)
	}
	if result.Written != 1 {
		t.Errorf("Written = %d, want 1", result.Written)
	}

	sessions, err := activitySvc.List(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 — the workout never became a session", len(sessions))
	}
	if sessions[0].Source != "apple_health" {
		t.Errorf("Source = %q, want %q — the CHECK constraint is still rejecting new providers",
			sessions[0].Source, "apple_health")
	}
}

// Re-sending a workout must not log it twice, the same way re-sending a reading
// does not. A phone bridge has no memory of what it already delivered.
func TestReingestingAWorkoutDoesNotDuplicateIt(t *testing.T) {
	svc, activitySvc, user := newServiceWithWorkouts(t)
	ctx := context.Background()

	workout := health.Workout{
		ActivityCode: "running_fast",
		ExternalID:   "hk-workout-1",
		StartedAt:    at("2026-08-15T07:00:00Z"),
		EndedAt:      at("2026-08-15T07:42:00Z"),
		Calories:     410,
	}
	for range 2 {
		if _, err := svc.IngestWorkouts(ctx, user.ID, "apple_health", []health.Workout{workout}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}

	sessions, err := activitySvc.List(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1 — the replay duplicated the workout", len(sessions))
	}
}

// An unknown activity code has no MET value, so its calorie estimate would be
// invented. Better to reject the payload than to store a fabricated number.
func TestAnUnknownActivityCodeIsRejected(t *testing.T) {
	svc, _, user := newServiceWithWorkouts(t)

	_, err := svc.IngestWorkouts(context.Background(), user.ID, "apple_health", []health.Workout{{
		ActivityCode: "competitive_napping",
		ExternalID:   "hk-workout-2",
		StartedAt:    at("2026-08-15T07:00:00Z"),
		EndedAt:      at("2026-08-15T07:42:00Z"),
	}})
	if err == nil {
		t.Fatal("err = nil, want a rejection for an unknown activity code")
	}
}

// One POST carries whatever the phone has: readings, workouts, or both.
func TestOnePostCanCarryReadingsAndWorkoutsTogether(t *testing.T) {
	svc, activitySvc, user := newServiceWithWorkouts(t)

	h := health.NewHandler(health.HandlerConfig{
		Service: svc,
		Auth:    stubAuth{token: "nk_testtoken", user: user},
	})

	rec := post(t, h, "/apple_health", "nk_testtoken", `{
		"readings":[{"metric":"steps","value":8432,"unit":"count","started_at":"2026-08-15T00:00:00Z"}],
		"workouts":[{"activity_code":"running_fast","external_id":"hk-1","started_at":"2026-08-15T07:00:00Z","ended_at":"2026-08-15T07:42:00Z","calories":410}]
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	sessions, err := activitySvc.List(context.Background(), user.ID, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1 — the workouts half of the payload was dropped", len(sessions))
	}

	stored, err := svc.Between(context.Background(), user.ID, "steps",
		at("2026-08-15T00:00:00Z"), at("2026-08-16T00:00:00Z"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(stored) != 1 {
		t.Errorf("got %d readings, want 1 — the readings half of the payload was dropped", len(stored))
	}
}
