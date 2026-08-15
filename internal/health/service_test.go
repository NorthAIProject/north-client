package health_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*health.Service, users.User) {
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

	return health.NewService(health.NewRepository(pool)), user
}

func at(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return ts
}

// The whole point of the ingest path: a phone bridge re-sends overlapping
// windows on every sync, so the same reading arrives many times and must not
// accumulate.
func TestReingestingTheSameWindowCorrectsRatherThanDuplicates(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	first := []health.Reading{{
		Metric:    "hrv_sdnn",
		Value:     42,
		Unit:      "ms",
		StartedAt: at("2026-08-15T02:00:00Z"),
	}}
	if _, err := svc.Ingest(ctx, user.ID, "apple_health", first); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	corrected := []health.Reading{{
		Metric:    "hrv_sdnn",
		Value:     47.5,
		Unit:      "ms",
		StartedAt: at("2026-08-15T02:00:00Z"),
	}}
	result, err := svc.Ingest(ctx, user.ID, "apple_health", corrected)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if result.Written != 1 {
		t.Errorf("Written = %d, want 1", result.Written)
	}

	readings, err := svc.Between(ctx, user.ID, "hrv_sdnn",
		at("2026-08-15T00:00:00Z"), at("2026-08-16T00:00:00Z"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("got %d readings, want 1 — the replay duplicated instead of correcting", len(readings))
	}
	if readings[0].Value != 47.5 {
		t.Errorf("Value = %v, want 47.5 — the correction did not win", readings[0].Value)
	}
}

// A payload is all-or-nothing. Half-applying a sync leaves a gap nobody can
// see, and the bridge has no way to know which half to resend.
func TestOneBadReadingRejectsTheWholePayload(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	readings := []health.Reading{
		{Metric: "steps", Value: 1200, Unit: "count", StartedAt: at("2026-08-15T01:00:00Z")},
		{Metric: "", Value: 5, Unit: "ms", StartedAt: at("2026-08-15T02:00:00Z")},
	}

	_, err := svc.Ingest(ctx, user.ID, "apple_health", readings)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}

	stored, err := svc.Between(ctx, user.ID, "steps",
		at("2026-08-15T00:00:00Z"), at("2026-08-16T00:00:00Z"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("got %d readings, want 0 — the good half of a rejected payload was kept", len(stored))
	}
}

// An interval that ends before it starts is not a clock problem to be tolerated;
// it means the payload was built wrong, and storing it would poison every
// window query that touches it.
func TestBackwardsWindowIsRejected(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	ended := at("2026-08-15T02:00:00Z")
	_, err := svc.Ingest(ctx, user.ID, "apple_health", []health.Reading{{
		Metric:    "sleep_deep",
		Value:     3600,
		Unit:      "seconds",
		StartedAt: at("2026-08-15T03:00:00Z"),
		EndedAt:   &ended,
	}})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
}

// Source is the caller's identity, not the payload's claim — it comes from the
// authenticated connection. An empty one would file readings under a provider
// nobody can later disconnect.
func TestIngestRequiresASource(t *testing.T) {
	svc, user := newService(t)

	_, err := svc.Ingest(context.Background(), user.ID, "", []health.Reading{{
		Metric:    "steps",
		Value:     10,
		Unit:      "count",
		StartedAt: at("2026-08-15T02:00:00Z"),
	}})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
}

// Disconnecting a provider forgets its readings and leaves every other
// provider's alone.
func TestForgettingASourceLeavesOtherSourcesIntact(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, err := svc.Ingest(ctx, user.ID, "apple_health", []health.Reading{{
		Metric: "steps", Value: 1000, Unit: "count", StartedAt: at("2026-08-15T01:00:00Z"),
	}}); err != nil {
		t.Fatalf("ingest apple: %v", err)
	}
	if _, err := svc.Ingest(ctx, user.ID, "whoop", []health.Reading{{
		Metric: "steps", Value: 2000, Unit: "count", StartedAt: at("2026-08-15T02:00:00Z"),
	}}); err != nil {
		t.Fatalf("ingest whoop: %v", err)
	}

	if err := svc.Forget(ctx, user.ID, "apple_health"); err != nil {
		t.Fatalf("forget: %v", err)
	}

	remaining, err := svc.Between(ctx, user.ID, "steps",
		at("2026-08-15T00:00:00Z"), at("2026-08-16T00:00:00Z"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("got %d readings, want 1", len(remaining))
	}
	if remaining[0].Source != "whoop" {
		t.Errorf("Source = %q, want %q — Forget took the wrong provider's data", remaining[0].Source, "whoop")
	}
}
