package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeProgress struct {
	sched progresscheck.Schedule
	due   bool
}

func (f *fakeProgress) Due(context.Context, users.User, time.Time) (progresscheck.Schedule, bool, error) {
	return f.sched, f.due, nil
}

// A due progress check is asked about once per due date, however many sweeps
// run; nothing is raised before it falls due, and a new date asks again.
func TestProgressCheckDueRaisesOncePerDate(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := mustOnboard(t, pool, seedUser(t, pool, "progress-due@north.test"), time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

	src := &fakeProgress{sched: progresscheck.Schedule{
		Cadence: progresscheck.Weekly, NextDueOn: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
	}}
	svc := prefsService(pool, now).WithProgressChecks(src)

	open := func() []nudges.Nudge {
		t.Helper()
		list, err := svc.ListOpen(ctx, user.ID, 50)
		if err != nil {
			t.Fatal(err)
		}
		var out []nudges.Nudge
		for _, n := range list {
			if n.Kind == nudges.KindProgressCheckDue {
				out = append(out, n)
			}
		}
		return out
	}

	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := open(); len(got) != 0 {
		t.Fatalf("raised before due: %+v", got)
	}

	src.due = true
	for range 2 {
		if _, err := svc.Evaluate(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	got := open()
	if len(got) != 1 || got[0].DedupeKey != "2026-10-15" || got[0].Href != "/app/progress" {
		t.Fatalf("open = %+v, want one nudge keyed 2026-10-15", got)
	}

	src.sched.NextDueOn = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := open(); len(got) != 2 {
		t.Fatalf("a new due date should ask again: %+v", got)
	}
}
