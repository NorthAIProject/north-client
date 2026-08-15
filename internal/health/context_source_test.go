package health_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/health"
)

func TestTheCoachIsToldAboutRecentDeviceReadings(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, err := svc.Ingest(ctx, user.ID, "apple_health", []health.Reading{
		{Metric: "hrv_sdnn", Value: 47, Unit: "ms", StartedAt: at("2026-08-14T03:00:00Z")},
		{Metric: "steps", Value: 8432, Unit: "count", StartedAt: at("2026-08-14T00:00:00Z")},
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	source := health.NewContextSource(svc, func() time.Time { return at("2026-08-15T00:00:00Z") })

	var built coach.Context
	if err := source.Collect(ctx, coach.ContextRequest{User: user}, &built); err != nil {
		t.Fatalf("collect: %v", err)
	}

	joined := strings.Join(built.DailySignals, "\n")
	if !strings.Contains(joined, "HRV") {
		t.Errorf("DailySignals = %v, want an HRV line", built.DailySignals)
	}
	if !strings.Contains(joined, "8,432") {
		t.Errorf("DailySignals = %v, want the step count", built.DailySignals)
	}
}

// An account with no device attached must add nothing at all, rather than a
// heading with nothing under it: every line here costs context window that the
// conversation itself could have used.
func TestAnAccountWithNoDeviceAddsNothingToTheContext(t *testing.T) {
	svc, user := newService(t)

	source := health.NewContextSource(svc, func() time.Time { return at("2026-08-15T00:00:00Z") })

	var built coach.Context
	if err := source.Collect(context.Background(), coach.ContextRequest{User: user}, &built); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(built.DailySignals) != 0 {
		t.Errorf("DailySignals = %v, want empty", built.DailySignals)
	}
}

func TestContextSourceIsNamedForLogsAndTiming(t *testing.T) {
	svc, _ := newService(t)

	if name := health.NewContextSource(svc, nil).Name(); name != "health" {
		t.Errorf("Name() = %q, want %q", name, "health")
	}
}
