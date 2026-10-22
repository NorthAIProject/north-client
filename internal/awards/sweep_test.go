package awards_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/users"
)

type oneUser struct{ u users.User }

func (o oneUser) ListOnboarded(_ context.Context, after uuid.UUID, _ int) ([]users.User, error) {
	if after != uuid.Nil {
		return nil, nil
	}
	return []users.User{o.u}, nil
}

func (e *env) sweep(at time.Time) {
	e.t.Helper()
	u := person(e.t, e)
	sw := awards.NewSweeper(oneUser{u}, e.svc, nil).WithClock(func() time.Time { return at })
	if err := sw.HandleSweep(context.Background(), nil); err != nil {
		e.t.Fatal(err)
	}
}

// 04:30 UTC on 1 Oct 2026.
var fourAM = time.Date(2026, 10, 1, 4, 30, 0, 0, time.UTC)

func TestSweepSkipsIdleAreas(t *testing.T) {
	e := newEnv(t, "UTC")
	// Ten completed sessions long ago: workouts_10 is met but nothing was
	// logged in the last day.
	e.sessions(10, 12)
	e.sweep(fourAM)
	if n := e.badgeRows(); n != 0 {
		t.Fatalf("sweep awarded %d badges for an idle area", n)
	}
	// A session ended an hour ago makes the area active: history counts.
	e.exec(`INSERT INTO activity_sessions (user_id, activity_code, status, weight_kg_snapshot, started_at, ended_at)
		VALUES ($1, 'running', 'completed', 70, $2::timestamptz - interval '2 hours', $2::timestamptz - interval '1 hour')`, fourAM)
	e.sweep(fourAM)
	if n := e.badgeRows(); n < 2 {
		t.Fatalf("sweep awarded %d badges, want workouts_1 and workouts_10", n)
	}
}

func TestSweepOnlyAtFourLocal(t *testing.T) {
	e := newEnv(t, "UTC")
	e.sessions(10, 12)
	e.exec(`INSERT INTO activity_sessions (user_id, activity_code, status, weight_kg_snapshot, started_at, ended_at)
		VALUES ($1, 'running', 'completed', 70, $2::timestamptz - interval '2 hours', $2::timestamptz - interval '1 hour')`, fourAM)
	e.sweep(fourAM.Add(2 * time.Hour))
	if n := e.badgeRows(); n != 0 {
		t.Fatalf("sweep ran outside 04:00 local and awarded %d", n)
	}
}
