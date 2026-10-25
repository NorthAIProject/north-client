package awards_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/xp"
)

func TestPickMonthly(t *testing.T) {
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) // 31 days
	sep := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)  // 30 days
	cases := []struct {
		name   string
		h      awards.MonthlyHistory
		start  time.Time
		metric awards.MonthlyMetric
		target int
	}{
		{"avg 4.0 workouts", awards.MonthlyHistory{Months: 3, Workouts: 12}, oct, awards.MetricWorkouts, 5},
		{"avg 3.9 workouts with a habit", awards.MonthlyHistory{Months: 10, Workouts: 39, HabitDays: 100, ActiveHabit: true}, oct, awards.MetricHabitDays, 12},
		{"no habit, 20 check-in days", awards.MonthlyHistory{Months: 1, CheckinDays: 20}, oct, awards.MetricCheckinDays, 23},
		{"30 check-in days in a 30-day month caps at 27", awards.MonthlyHistory{Months: 1, CheckinDays: 30}, sep, awards.MetricCheckinDays, 27},
		{"no history", awards.MonthlyHistory{}, oct, awards.MetricCheckinDays, 8},
		{"no history with a habit", awards.MonthlyHistory{ActiveHabit: true}, oct, awards.MetricHabitDays, 8},
		{"workouts are not days-limited", awards.MonthlyHistory{Months: 1, Workouts: 20}, oct, awards.MetricWorkouts, 23},
		{"workouts capped by the month", awards.MonthlyHistory{Months: 1, Workouts: 40}, sep, awards.MetricWorkouts, 27},
		{"months with nothing logged get the starter", awards.MonthlyHistory{Months: 2}, oct, awards.MetricCheckinDays, 8},
		{"a single check-in day rounds up to 2", awards.MonthlyHistory{Months: 1, CheckinDays: 1}, oct, awards.MetricCheckinDays, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, n := awards.PickMonthly(c.h, c.start)
			if m != c.metric || n != c.target {
				t.Fatalf("got %s %d, want %s %d", m, n, c.metric, c.target)
			}
		})
	}
}

// oldUser makes the account old enough for history to count.
func (e *env) oldAccount() {
	e.exec(`UPDATE users SET created_at = '2025-01-01' WHERE id = $1`)
}

func (e *env) session(endedAt time.Time) {
	e.exec(`INSERT INTO activity_sessions (user_id, activity_code, status, weight_kg_snapshot, started_at, ended_at)
		VALUES ($1, 'running', 'completed', 70, $2::timestamptz - interval '30 minutes', $2)`, endedAt)
}

func (e *env) monthly(at time.Time) awards.MonthlyChallenge {
	e.t.Helper()
	c, err := e.svc.Monthly(context.Background(), e.user, at)
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *env) metRows() []string {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT source_key FROM achievements WHERE user_id=$1 AND kind='monthly_challenge_met' ORDER BY 1`, e.user)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, k)
	}
	return out
}

func TestMonthUsesLocalTime(t *testing.T) {
	cases := []struct {
		name, tz string
		endedAt  time.Time // UTC instant
		inOct    bool      // whether the local date is in October
	}{
		// 23:30 on 31 October in Lisbon. The clocks went back on the 25th,
		// so UTC agrees; the zones below are the ones where it does not.
		{"Lisbon 23:30 on 31 Oct", "Europe/Lisbon", time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC), true},
		// 21:30 PDT on 31 October is already 1 November in UTC.
		{"Los Angeles 21:30 on 31 Oct", "America/Los_Angeles", time.Date(2026, 11, 1, 4, 30, 0, 0, time.UTC), true},
		// 00:30 NZDT on 1 November is still 31 October in UTC.
		{"Auckland 00:30 on 1 Nov", "Pacific/Auckland", time.Date(2026, 10, 31, 11, 30, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, c.tz)
			e.oldAccount()
			e.session(c.endedAt)
			// Fix both months' metric so only the month boundary is under test.
			e.exec(`INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, '2026-10-01', 'workouts', 5), ($1, '2026-11-01', 'workouts', 5)`)
			oct := e.monthly(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
			nov := e.monthly(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
			wantOct, wantNov := 1, 0
			if !c.inOct {
				wantOct, wantNov = 0, 1
			}
			if oct.Key != "2026-10" || nov.Key != "2026-11" || oct.Progress != wantOct || nov.Progress != wantNov {
				t.Fatalf("october %s=%d (want %d), november %s=%d (want %d)", oct.Key, oct.Progress, wantOct, nov.Key, nov.Progress, wantNov)
			}
		})
	}
}

func TestChallengeCreatedOnceAndFixed(t *testing.T) {
	e := newEnv(t, "UTC")
	e.oldAccount()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	got := make([]awards.MonthlyChallenge, 2)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := e.svc.Monthly(context.Background(), e.user, at)
			if err != nil {
				t.Error(err)
			}
			got[i] = c
		}()
	}
	wg.Wait()
	if got[0].Metric != got[1].Metric || got[0].Target != got[1].Target {
		t.Fatalf("racing views disagree: %+v vs %+v", got[0], got[1])
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM monthly_challenges WHERE user_id=$1`, e.user).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, err %v; want 1", n, err)
	}

	// 30 sessions a month ago would now pick workouts, but the month is fixed.
	for i := 0; i < 30; i++ {
		e.session(time.Date(2026, 9, 1+i%28, 9, 0, 0, 0, time.UTC))
	}
	again := e.monthly(at)
	if again.Metric != got[0].Metric || again.Target != got[0].Target {
		t.Fatalf("challenge changed: %+v then %+v", got[0], again)
	}
	// The next month does pick it up.
	if nov := e.monthly(time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)); nov.Metric != awards.MetricWorkouts {
		t.Fatalf("november metric = %s, want workouts", nov.Metric)
	}
}

func TestTargetFloorsAndCaps(t *testing.T) {
	t.Run("brand-new user gets the starter floor", func(t *testing.T) {
		e := newEnv(t, "UTC")
		c := e.monthly(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
		if c.Metric != awards.MetricCheckinDays || c.Target != 8 {
			t.Fatalf("got %s %d", c.Metric, c.Target)
		}
	})
	t.Run("a habit makes the floor habit days", func(t *testing.T) {
		e := newEnv(t, "UTC")
		e.exec(`INSERT INTO habits (user_id, name) VALUES ($1, 'Stretch')`)
		c := e.monthly(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
		if c.Metric != awards.MetricHabitDays || c.Target != 8 {
			t.Fatalf("got %s %d", c.Metric, c.Target)
		}
	})
	t.Run("joined on the 28th of a 31-day month", func(t *testing.T) {
		e := newEnv(t, "UTC")
		e.exec(`UPDATE users SET created_at = '2026-10-28 08:00+00' WHERE id = $1`)
		c := e.monthly(time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC))
		if c.Metric != awards.MetricCheckinDays || c.Target != 4 {
			t.Fatalf("got %s %d, want checkin_days 4", c.Metric, c.Target)
		}
	})
	t.Run("months before the account existed do not count", func(t *testing.T) {
		e := newEnv(t, "UTC")
		e.exec(`UPDATE users SET created_at = '2026-09-20 08:00+00' WHERE id = $1`)
		e.checkIns(0, 0) // a January check-in, before the account
		e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy) SELECT $1, d::date, 3, 3 FROM generate_series(date '2026-09-20', date '2026-09-29', interval '1 day') d`)
		c := e.monthly(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC))
		// September is the only month: 10 days → ceil(11.5) = 12.
		if c.Metric != awards.MetricCheckinDays || c.Target != 12 {
			t.Fatalf("got %s %d, want checkin_days 12", c.Metric, c.Target)
		}
	})
}

func TestMonthlyMetOnce(t *testing.T) {
	e := newEnv(t, "UTC")
	e.oldAccount()
	clock := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	e.svc.SetClock(func() time.Time { return clock })
	ctx := context.Background()

	e.svc.Check(ctx, e.user, clock, award.Consistency)
	if got := e.metRows(); len(got) != 0 {
		t.Fatalf("met before any progress: %v", got)
	}
	e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy) SELECT $1, d::date, 3, 3 FROM generate_series(date '2026-10-01', date '2026-10-08', interval '1 day') d`)

	user, err := users.NewService(users.NewRepository(e.pool)).ByID(ctx, e.user)
	if err != nil {
		t.Fatal(err)
	}
	x := xp.NewService(e.pool, checkins.NewService(checkins.NewRepository(e.pool), goals.NewService(goals.NewRepository(e.pool))))
	before, err := x.Summary(ctx, user, clock)
	if err != nil {
		t.Fatal(err)
	}

	e.svc.Check(ctx, e.user, clock, award.Training)
	e.svc.Check(ctx, e.user, clock, award.Training)
	if got := e.metRows(); len(got) != 1 || got[0] != "2026-10" {
		t.Fatalf("met rows = %v, want [2026-10]", got)
	}
	var category string
	var private bool
	if err = e.pool.QueryRow(ctx, `SELECT category, private FROM achievements WHERE user_id=$1 AND kind='monthly_challenge_met'`, e.user).Scan(&category, &private); err != nil {
		t.Fatal(err)
	}
	if category != "awards" || private {
		t.Fatalf("category %q private %v", category, private)
	}
	after, err := x.Summary(ctx, user, clock)
	if err != nil {
		t.Fatal(err)
	}
	if before.Total != after.Total || before.WeekTotal != after.WeekTotal {
		t.Fatalf("xp moved: %+v -> %+v", before, after)
	}
	medals, err := e.svc.MonthlyMedals(ctx, e.user)
	if err != nil || len(medals) != 1 || medals[0].Key != "2026-10" || medals[0].Metric != awards.MetricCheckinDays || medals[0].Target != 8 {
		t.Fatalf("medals = %+v, err %v", medals, err)
	}
	if earned, err := e.svc.Earned(ctx, e.user); err != nil || len(earned) != 0 {
		t.Fatalf("monthly leaked into Earned: %v %v", earned, err)
	}
}

func TestBackdatedEventNeverGrantsPastMonth(t *testing.T) {
	e := newEnv(t, "UTC")
	e.oldAccount()
	// October's challenge exists and is one session away.
	e.exec(`INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, '2026-10-01', 'workouts', 1)`)
	clock := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	e.svc.SetClock(func() time.Time { return clock })

	oct := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	e.session(oct) // imported in November
	e.svc.Check(context.Background(), e.user, oct, award.Training)

	if got := e.metRows(); len(got) != 0 {
		t.Fatalf("a past month was granted: %v", got)
	}
	if nov := e.monthly(clock); nov.Key != "2026-11" || nov.Met {
		t.Fatalf("november = %+v", nov)
	}
}
