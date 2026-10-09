package lighterday_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/lighterday"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Wednesday 7 October 2026, 07:30: a morning with readings.
var morning = time.Date(2026, 10, 7, 7, 30, 0, 0, time.UTC)

// readings is a Health store: a two-week baseline and this morning's value.
type readings struct{ hrvToday, rhrToday float64 }

func (r readings) Between(_ context.Context, _ uuid.UUID, metric string, _, _ time.Time) ([]health.Stored, error) {
	base, today := 52.0, r.hrvToday
	if metric == "resting_heart_rate" {
		base, today = 56, r.rhrToday
	}
	var out []health.Stored
	for d := 2; d <= 14; d++ {
		out = append(out, health.Stored{Value: base, StartedAt: morning.AddDate(0, 0, -d)})
	}
	return append(out, health.Stored{Value: today, StartedAt: morning.Add(-time.Hour)}), nil
}

type session struct{ due, done bool }

func (s session) DueToday(context.Context, users.User, time.Time) (string, string, bool, error) {
	return "Lower body", "/app/training/x", s.due, nil
}

func (s session) CompletedToday(context.Context, users.User, time.Time) (bool, string, error) {
	return s.done, "", nil
}

func user(t *testing.T) (users.User, func(h lighterday.HealthReader, s lighterday.SessionReader) *lighterday.Service) {
	t.Helper()
	pool := testdb.New(t)
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: "lighter@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Ana", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u, func(h lighterday.HealthReader, s lighterday.SessionReader) *lighterday.Service {
		return lighterday.NewService(pool, h, s).WithClock(func() time.Time { return morning })
	}
}

// Low HRV and a session to do: offered; taking it lighter is remembered for
// today and the offer goes away.
func TestOfferedOnALowMorning(t *testing.T) {
	ctx := context.Background()
	u, build := user(t)
	svc := build(readings{hrvToday: 38, rhrToday: 56}, session{due: true})

	today, err := svc.Today(ctx, u)
	if err != nil || !today.Offered() || !today.Readiness.Low {
		t.Fatalf("today = %+v, %v", today, err)
	}
	if _, err = svc.Choose(ctx, u, lighterday.ChoiceLighter); err != nil {
		t.Fatal(err)
	}
	today, _ = svc.Today(ctx, u)
	if today.Offered() || !today.Lighter() {
		t.Fatalf("after choosing: %+v", today)
	}
	if on, _ := svc.LighterOn(ctx, u, morning.Add(10*time.Hour)); !on {
		t.Fatal("today is not lighter later the same day")
	}
	if on, _ := svc.LighterOn(ctx, u, morning.AddDate(0, 0, 1)); on {
		t.Fatal("tomorrow is lighter too")
	}
}

// A normal morning, a rest day, or a session already done: no offer.
func TestNotOffered(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		h readings
		s session
	}{
		"normal morning":       {readings{hrvToday: 51, rhrToday: 57}, session{due: true}},
		"rest day":             {readings{hrvToday: 38, rhrToday: 56}, session{}},
		"session already done": {readings{hrvToday: 38, rhrToday: 56}, session{due: true, done: true}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			u, build := user(t)
			today, err := build(c.h, c.s).Today(ctx, u)
			if err != nil || today.Offered() {
				t.Fatalf("offered on a %s: %+v, %v", name, today, err)
			}
		})
	}
}

// High resting heart rate alone is enough; lighter needs a session to do.
func TestRestingHeartRateAndRefusals(t *testing.T) {
	ctx := context.Background()
	u, build := user(t)
	if today, _ := build(readings{hrvToday: 52, rhrToday: 62}, session{due: true}).Today(ctx, u); !today.Offered() {
		t.Fatalf("high resting heart rate not offered: %+v", today)
	}
	rest := build(readings{hrvToday: 38, rhrToday: 56}, session{})
	if _, err := rest.Choose(ctx, u, lighterday.ChoiceLighter); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("lighter on a rest day: %v", err)
	}
	if _, err := rest.Choose(ctx, u, "skip"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("unknown choice: %v", err)
	}
}
