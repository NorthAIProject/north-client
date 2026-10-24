package awards_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/awards"
)

func TestReachableTarget(t *testing.T) {
	for _, c := range []struct{ target, done, left, want int }{
		{28, 24, 7, 28}, // normal: the cap is above the target
		{28, 0, 7, 7},   // nothing logged yet: only what the month can still deliver
		{28, 24, 2, 26}, // the cap wins over the target
		{5, 0, 0, 1},    // never below 1
	} {
		if got := awards.ReachableTarget(c.target, c.done, c.left); got != c.want {
			t.Errorf("ReachableTarget(%d,%d,%d) = %d, want %d", c.target, c.done, c.left, got, c.want)
		}
	}
}

// A row created late in the month counts the days already logged: a daily
// check-in user gets the normal target and is not met on creation.
func TestRowCreatedOnThe25thIsNotMetOnCreation(t *testing.T) {
	e := newEnv(t, "UTC")
	e.exec(`UPDATE users SET created_at = '2026-07-01' WHERE id = $1`)
	e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy) SELECT $1, d::date, 3, 3 FROM generate_series(date '2026-07-01', date '2026-10-24', interval '1 day') d`)
	c := e.monthly(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC))
	if c.Metric != awards.MetricCheckinDays || c.Progress != 24 {
		t.Fatalf("got %s progress %d", c.Metric, c.Progress)
	}
	// 31-day month: the days_in_month-3 cap, not the 7 days left.
	if c.Target != 28 || c.Met || c.MetAt != nil {
		t.Fatalf("target %d met %v: want 28 and not met", c.Target, c.Met)
	}
	if got := e.metRows(); len(got) != 0 {
		t.Fatalf("met rows on creation: %v", got)
	}
}

func TestBrandNewUserMidMonthStillGetsAReachableTarget(t *testing.T) {
	e := newEnv(t, "UTC")
	e.exec(`UPDATE users SET created_at = '2026-10-20 08:00+00' WHERE id = $1`)
	c := e.monthly(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))
	daysLeft := 12
	if c.Target < 1 || c.Target > c.Progress+daysLeft || c.Met {
		t.Fatalf("target %d progress %d: not reachable, or met on creation", c.Target, c.Progress)
	}
}

// The owner's path writes the met row as soon as it would show "met"; a peek
// never writes.
func TestOwnerPathWritesMetOnOpenAndPeekNeverDoes(t *testing.T) {
	e := newEnv(t, "UTC")
	ctx := context.Background()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	e.exec(`INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, '2026-10-01', 'checkin_days', 3)`)
	e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy) SELECT $1, d::date, 3, 3 FROM generate_series(date '2026-10-01', date '2026-10-03', interval '1 day') d`)

	c, ok, err := e.svc.PeekMonthly(ctx, e.user, at)
	if err != nil || !ok || !c.Met {
		t.Fatalf("peek = %+v %v %v", c, ok, err)
	}
	if got := e.metRows(); len(got) != 0 {
		t.Fatalf("a peek wrote a met row: %v", got)
	}
	c = e.monthly(at)
	if got := e.metRows(); len(got) != 1 || got[0] != "2026-10" || c.MetAt == nil {
		t.Fatalf("owner open: met rows %v, MetAt %v", got, c.MetAt)
	}
	e.monthly(at) // a repeat is a no-op
	if got := e.metRows(); len(got) != 1 {
		t.Fatalf("repeat wrote again: %v", got)
	}
}
