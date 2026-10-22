package health_test

import (
	"context"
	"testing"

	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// captures stands in for PostHog; only Enqueue is called.
type captures struct {
	posthog.Client
	events []posthog.Capture
}

func (c *captures) Enqueue(msg posthog.Message) error {
	if e, ok := msg.(posthog.Capture); ok {
		c.events = append(c.events, e)
	}
	return nil
}

// Apple Health has no connect button: the app just starts syncing. The first
// sync is the connection, and the phone re-sends every few minutes, so only
// that first one may count — otherwise every iPhone user is "connected" a
// hundred times a day and the source step of the funnel means nothing.
func TestOnlyTheFirstAppleHealthDeliveryCountsAsConnected(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        "first-health@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Ana",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := &captures{}
	svc := health.NewService(health.NewRepository(pool)).WithFunnel(analytics.New(rec))

	sync := func(value float64, when string) {
		t.Helper()
		if _, err := svc.Ingest(ctx, user.ID, health.SourceAppleHealth, []health.Reading{{
			Metric: "steps", Value: value, Unit: "count", StartedAt: at(when),
		}}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}
	sync(1200, "2026-10-20T10:00:00Z")
	sync(3400, "2026-10-21T10:00:00Z")

	// A manual reading is not the phone connecting.
	if _, err := svc.Ingest(ctx, user.ID, health.SourceManual, []health.Reading{{
		Metric: "steps", Value: 70, Unit: "count", StartedAt: at("2026-10-21T08:00:00Z"),
	}}); err != nil {
		t.Fatalf("manual ingest: %v", err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("captured %d events, want exactly 1", len(rec.events))
	}
	if rec.events[0].Event != "source_connected" || rec.events[0].Properties["source"] != "apple_health" {
		t.Errorf("event = %q %v, want source_connected apple_health", rec.events[0].Event, rec.events[0].Properties)
	}
}


