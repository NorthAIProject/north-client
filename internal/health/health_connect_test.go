package health_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/analytics"
	awardsdb "github.com/NorthAIProject/north-client/internal/awards/db"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The Android app reads Health Connect and sends it the way the iPhone sends
// Apple Health: no connect step, a re-send every few minutes. It must count as
// connected once, replace a re-sent day rather than add to it, and earn the
// same connected-days award.
func TestHealthConnectIsAPhoneSourceLikeAppleHealth(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        "health-connect@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Rui",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := &captures{}
	svc := health.NewService(health.NewRepository(pool)).WithFunnel(analytics.New(rec))

	dayEnd := at("2026-10-22T00:00:00Z")
	sync := func(value float64) {
		t.Helper()
		if _, err := svc.Ingest(ctx, user.ID, health.SourceHealthConnect, []health.Reading{{
			Metric: "steps", Value: value, Unit: "count",
			StartedAt: at("2026-10-21T00:00:00Z"), EndedAt: &dayEnd,
		}}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}
	sync(3400)
	sync(5100) // the same day, re-sent later with more steps

	stored, err := svc.Between(ctx, user.ID, "steps", at("2026-10-20T00:00:00Z"), at("2026-10-23T00:00:00Z"))
	if err != nil {
		t.Fatalf("between: %v", err)
	}
	if len(stored) != 1 || stored[0].Value != 5100 || stored[0].Source != health.SourceHealthConnect {
		t.Fatalf("stored = %+v, want one health_connect reading of 5100", stored)
	}

	if len(rec.events) != 1 || rec.events[0].Properties["source"] != analytics.SourceHealthConnect {
		t.Fatalf("events = %+v, want one source_connected health_connect", rec.events)
	}

	days, err := awardsdb.New(pool).CountConnectedDays(ctx, awardsdb.CountConnectedDaysParams{Tz: "Europe/Lisbon", UserID: user.ID})
	if err != nil {
		t.Fatalf("count connected days: %v", err)
	}
	if days != 1 {
		t.Errorf("connected days = %d, want 1", days)
	}
}
