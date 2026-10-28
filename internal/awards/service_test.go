package awards_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// now is a Wednesday evening, UTC.
var now = time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	user uuid.UUID
	svc  *awards.Service
}

func newUser(t *testing.T, pool *pgxpool.Pool, email, tz string) uuid.UUID {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Pat", Timezone: tz,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func newEnv(t *testing.T, tz string) *env {
	t.Helper()
	pool := testdb.New(t)
	return &env{t: t, pool: pool, user: newUser(t, pool, "pat@example.com", tz), svc: awards.NewService(awards.NewRepository(pool))}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, append([]any{e.user}, args...)...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

func (e *env) evaluate(areas ...award.Area) []awards.Earned {
	e.t.Helper()
	got, err := e.svc.Evaluate(context.Background(), e.user, now, areas...)
	if err != nil {
		e.t.Fatal(err)
	}
	return got
}

func ids(es []awards.Earned) []string {
	out := []string{}
	for _, x := range es {
		out = append(out, x.Badge.ID)
	}
	sort.Strings(out)
	return out
}

func (e *env) badgeRows() int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM achievements WHERE user_id=$1 AND kind='badge_earned'`, e.user).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Seeds. Day i of a run is 2026-01-01 + i days, so run i and i+1 touch.

func (e *env) sessions(n int, hourUTC int) {
	e.exec(`INSERT INTO activity_sessions (user_id, activity_code, status, weight_kg_snapshot, started_at, ended_at)
		SELECT $1, 'running', 'completed', 70, d + make_interval(hours => $2::int), d + make_interval(hours => $2::int, mins => 30)
		FROM generate_series(timestamp '2026-01-01', timestamp '2026-01-01' + make_interval(days => $3::int - 1), interval '1 day') d`, hourUTC, n)
}

func (e *env) checkIns(from, to int) {
	e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy)
		SELECT $1, date '2026-01-01' + i, 3, 3 FROM generate_series($2::int, $3::int) i`, from, to)
}

func (e *env) food(from, to int, protein float64) {
	e.exec(`INSERT INTO food_logs (user_id, log_date, label, calories, protein_g, fat_g, carbs_g)
		SELECT $1, date '2026-01-01' + i, 'meal', 500, $4::float8, 10, 50 FROM generate_series($2::int, $3::int) i`, from, to, protein)
}

func (e *env) setLog(day int, kg float64, reps int, kind string) {
	e.exec(`INSERT INTO set_logs (user_id, log_date, exercise_slug, exercise_name, set_number, weight_kg, reps, kind, performed_at)
		VALUES ($1, date '2026-01-01' + $2::int, 'bench', 'Bench', 1, $3, $4, $5, timestamp '2026-01-01 10:00' + make_interval(days => $2::int, secs => $6::int))`,
		day, kg, reps, kind, int(kg))
}

func (e *env) habit(days string) uuid.UUID {
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO habits (user_id, name, days_of_week) VALUES ($1, 'Walk', `+days+`) RETURNING id`, e.user).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) goal(status, closed, target string) uuid.UUID {
	var id uuid.UUID
	q := `INSERT INTO goals (user_id, title, status, closed_at, target_date) VALUES ($1, 'Goal', $2, NULLIF($3,'')::timestamptz, NULLIF($4,'')::date) RETURNING id`
	if err := e.pool.QueryRow(context.Background(), q, e.user, status, closed, target).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) follower(email, status string, blocked bool) {
	other := newUser(e.t, e.pool, email, "UTC")
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO follows (follower_id, followee_id, status) VALUES ($1,$2,$3)`, other, e.user, status); err != nil {
		e.t.Fatal(err)
	}
	if blocked {
		if _, err := e.pool.Exec(context.Background(), `INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2)`, e.user, other); err != nil {
			e.t.Fatal(err)
		}
	}
}

type rule struct {
	id    string
	below func(e *env)
	add   func(e *env)
}

func rules() []rule {
	workouts := func(id string, n int) rule {
		return rule{id, func(e *env) { e.sessions(n-1, 12) }, func(e *env) { e.sessions(n, 12) }}
	}
	// The run of n days, 1..n-1 below and the nth day closing it.
	run := func(id string, n int, seed func(e *env, from, to int)) rule {
		return rule{id, func(e *env) { seed(e, 1, n-1) }, func(e *env) { seed(e, 1, n) }}
	}
	checks := func(e *env, from, to int) { e.checkIns(from, to) }
	foods := func(e *env, from, to int) { e.food(from, to, 20) }
	waters := func(e *env, from, to int) {
		e.exec(`INSERT INTO hydration_logs (user_id, log_date, amount_ml) SELECT $1, date '2026-01-01' + i, 2000 FROM generate_series($2::int, $3::int) i`, from, to)
	}
	sleeps := func(e *env, from, to int) {
		e.exec(`INSERT INTO sleep_logs (user_id, local_date, duration_minutes) SELECT $1, date '2026-01-01' + i, 480 FROM generate_series($2::int, $3::int) i`, from, to)
	}
	journals := func(n int) rule {
		ins := func(e *env, k int) {
			e.exec(`INSERT INTO journal_entries (user_id, content) SELECT $1, 'x' FROM generate_series(1, $2::int)`, k)
		}
		return rule{fmt.Sprintf("journal_%d", n), func(e *env) { ins(e, n-1) }, func(e *env) { ins(e, n) }}
	}
	return []rule{
		workouts("workouts_1", 1), workouts("workouts_10", 10), workouts("workouts_50", 50), workouts("workouts_100", 100),
		{
			"early_bird",
			func(e *env) { e.sessions(9, 6); e.sessions(1, 8) }, // 9 early, plus a 08:00 one (same date, extra row)
			func(e *env) { e.sessions(10, 6) },
		},
		{
			"pr_1",
			// Ramping up within the first session is the baseline, not a record.
			func(e *env) { e.setLog(1, 60, 5, "work"); e.setLog(1, 70, 5, "work"); e.setLog(1, 80, 5, "work") },
			func(e *env) { e.setLog(1, 60, 5, "work"); e.setLog(2, 100, 5, "work") },
		},
		{
			"pr_10",
			func(e *env) {
				for d := 1; d <= 10; d++ { // day 1 is the baseline: 9 records follow
					e.setLog(d, float64(50+d*5), 5, "work")
				}
			},
			func(e *env) {
				for d := 1; d <= 11; d++ {
					e.setLog(d, float64(50+d*5), 5, "work")
				}
			},
		},
		{
			"century",
			func(e *env) { e.setLog(1, 99.5, 5, "work"); e.setLog(2, 140, 5, "drop"); e.setLog(3, 100, 5, "warmup") },
			func(e *env) { e.setLog(1, 100, 1, "work") },
		},
		run("streak_7", 7, checks), run("streak_30", 30, checks), run("streak_100", 100, checks), run("streak_365", 365, checks),
		{
			"habit_locked_in",
			func(e *env) {
				h := e.habit("'{0,1,2,3,4,5,6}'")
				// Weeks from Monday 2026-01-05: three full, the fourth missing Sunday.
				e.exec(`INSERT INTO habit_completions (user_id, habit_id, local_date) SELECT $1, $2, date '2026-01-05' + i FROM generate_series(0, 26) i`, h)
			},
			func(e *env) {
				h := e.habit("'{0,1,2,3,4,5,6}'")
				e.exec(`INSERT INTO habit_completions (user_id, habit_id, local_date) SELECT $1, $2, date '2026-01-05' + i FROM generate_series(0, 27) i`, h)
			},
		},
		{
			"habit_100",
			func(e *env) {
				h := e.habit("'{1}'")
				e.exec(`INSERT INTO habit_completions (user_id, habit_id, local_date) SELECT $1, $2, date '2026-01-01' + i FROM generate_series(1, 99) i`, h)
			},
			func(e *env) {
				h := e.habit("'{1}'")
				e.exec(`INSERT INTO habit_completions (user_id, habit_id, local_date) SELECT $1, $2, date '2026-01-01' + i FROM generate_series(1, 100) i`, h)
			},
		},
		run("meals_week", 7, foods),
		{
			"protein_10",
			func(e *env) {
				e.exec(`INSERT INTO user_macro_plans (user_id, weight_kg, height_cm, age, sex, activity_level, goal, macro_split, bmr, tdee, calorie_goal, protein_g, fat_g, carb_g, is_current)
					VALUES ($1, 80, 180, 30, 'male', 'light', 'maintenance', 'moderate_carb', 1800, 2400, 2400, 100, 70, 250, true)`)
				e.food(1, 9, 100)
				e.food(10, 10, 99) // one short
			},
			func(e *env) {
				e.exec(`INSERT INTO user_macro_plans (user_id, weight_kg, height_cm, age, sex, activity_level, goal, macro_split, bmr, tdee, calorie_goal, protein_g, fat_g, carb_g, is_current)
					VALUES ($1, 80, 180, 30, 'male', 'light', 'maintenance', 'moderate_carb', 1800, 2400, 2400, 100, 70, 250, true)`)
				e.food(1, 10, 100)
			},
		},
		run("water_week", 7, waters), run("water_30", 30, waters),
		{
			"rested_week",
			func(e *env) { // Monday 2026-01-05 to Saturday, plus a short Sunday
				e.exec(`INSERT INTO sleep_logs (user_id, local_date, duration_minutes) SELECT $1, date '2026-01-05' + i, 480 FROM generate_series(0, 5) i`)
				e.exec(`INSERT INTO sleep_logs (user_id, local_date, duration_minutes) VALUES ($1, '2026-01-11', 400)`)
			},
			func(e *env) {
				e.exec(`INSERT INTO sleep_logs (user_id, local_date, duration_minutes) SELECT $1, date '2026-01-05' + i, 480 FROM generate_series(0, 6) i`)
			},
		},
		run("nights_30", 30, sleeps),
		journals(1), journals(10),
		{
			"milestone_1",
			func(e *env) {
				g := e.goal("active", "", "")
				e.exec(`INSERT INTO goal_milestones (user_id, goal_id, title, status) VALUES ($1, $2, 'm', 'open')`, g)
			},
			func(e *env) {
				g := e.goal("active", "", "")
				e.exec(`INSERT INTO goal_milestones (user_id, goal_id, title, status, completed_at) VALUES ($1, $2, 'm', 'completed', now())`, g)
			},
		},
		{
			"goal_on_time",
			func(e *env) {
				e.goal("achieved", "2026-03-02 10:00+00", "2026-03-01") // a day late
				e.goal("active", "", "2026-03-01")
				e.goal("achieved", "2026-03-02 10:00+00", "")
			},
			func(e *env) { e.goal("achieved", "2026-03-01 23:00+00", "2026-03-01") },
		},
		{
			"reviews_8",
			func(e *env) {
				e.exec(`INSERT INTO weekly_focus (user_id, week_start) SELECT $1, date '2026-01-05' + i*7 FROM generate_series(0, 6) i`)
			},
			func(e *env) {
				e.exec(`INSERT INTO weekly_focus (user_id, week_start) SELECT $1, date '2026-01-05' + i*7 FROM generate_series(0, 7) i`)
			},
		},
		{
			"checks_4",
			func(e *env) { // three weekly checks, then a two-month gap
				e.exec(`INSERT INTO progress_checks (user_id, measured_on) SELECT $1, date '2026-01-05' + i*7 FROM generate_series(0, 2) i`)
				e.exec(`INSERT INTO progress_checks (user_id, measured_on) VALUES ($1, '2026-04-01')`)
			},
			func(e *env) {
				e.exec(`INSERT INTO progress_checks (user_id, measured_on) SELECT $1, date '2026-01-05' + i*7 FROM generate_series(0, 3) i`)
			},
		},
		{
			"connected_30",
			func(e *env) {
				e.exec(`INSERT INTO health_metrics (user_id, source, metric, value, unit, started_at)
					SELECT $1, 'apple_health', 'steps', 1000, 'count', timestamptz '2026-01-01 12:00+00' + make_interval(days => i) FROM generate_series(1, 29) i`)
				e.exec(`INSERT INTO health_metrics (user_id, source, metric, value, unit, started_at) VALUES ($1, 'other', 'steps', 1, 'count', '2026-02-20 12:00+00')`)
				// Two samples one day count once.
				e.exec(`INSERT INTO health_metrics (user_id, source, metric, value, unit, started_at) VALUES ($1, 'apple_health', 'hr', 60, 'bpm', '2026-01-02 15:00+00')`)
			},
			func(e *env) {
				e.exec(`INSERT INTO health_metrics (user_id, source, metric, value, unit, started_at)
					SELECT $1, 'apple_health', 'steps', 1000, 'count', timestamptz '2026-01-01 12:00+00' + make_interval(days => i) FROM generate_series(1, 30) i`)
			},
		},
		{
			"fasts_10",
			func(e *env) {
				e.exec(`INSERT INTO fasting_sessions (user_id, started_at, ended_at, target_hours)
					SELECT $1, timestamptz '2026-01-01 20:00+00' + make_interval(days => i), timestamptz '2026-01-02 12:00+00' + make_interval(days => i), 16 FROM generate_series(1, 9) i`)
				e.exec(`INSERT INTO fasting_sessions (user_id, started_at, ended_at, target_hours) VALUES ($1, '2026-03-01 20:00+00', '2026-03-02 06:00+00', 16)`) // 10 h
				e.exec(`INSERT INTO fasting_sessions (user_id, started_at, target_hours) VALUES ($1, '2026-04-01 20:00+00', 16)`)                                  // still going
			},
			func(e *env) {
				e.exec(`INSERT INTO fasting_sessions (user_id, started_at, ended_at, target_hours)
					SELECT $1, timestamptz '2026-01-01 20:00+00' + make_interval(days => i), timestamptz '2026-01-02 12:00+00' + make_interval(days => i), 16 FROM generate_series(1, 10) i`)
			},
		},
		{
			"first_follower",
			func(e *env) {
				e.follower("a@example.com", "pending", false)
				e.follower("b@example.com", "accepted", true)
			},
			func(e *env) { e.follower("c@example.com", "accepted", false) },
		},
		{
			"crew_challenge",
			func(e *env) {},
			func(e *env) {
				e.exec(`INSERT INTO achievements (user_id, category, kind, title, source_key, occurred_at) VALUES ($1, 'goals', 'crew_challenge_met', 'x', 'k', now())`)
			},
		},
	}
}

func TestEveryBadgeButOneHasARule(t *testing.T) {
	covered := map[string]bool{"listened_to_body": true}
	for _, r := range rules() {
		covered[r.id] = true
	}
	for _, b := range award.Catalog() {
		if !covered[b.ID] {
			t.Errorf("no rule test for %s", b.ID)
		}
	}
}

// One row per badge: just below the target earns nothing; one more qualifying
// row earns that badge.
func TestRules(t *testing.T) {
	for _, r := range rules() {
		t.Run(r.id, func(t *testing.T) {
			t.Parallel()
			b, ok := award.ByID(r.id)
			if !ok {
				t.Fatalf("unknown badge %s", r.id)
			}
			below := newEnv(t, "UTC")
			r.below(below)
			for _, got := range below.evaluate(b.Area) {
				if got.Badge.ID == r.id {
					t.Fatalf("%s earned just below its target", r.id)
				}
			}
			above := newEnv(t, "UTC")
			r.add(above)
			found := false
			for _, got := range above.evaluate(b.Area) {
				if got.Badge.ID == r.id {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s not earned at its target", r.id)
			}
		})
	}
}

func TestNoAwardWithoutEvent(t *testing.T) {
	e := newEnv(t, "UTC")
	e.sessions(120, 12)
	e.checkIns(1, 400)
	if n := e.badgeRows(); n != 0 {
		t.Fatalf("badge rows = %d with nobody calling Evaluate", n)
	}
	// Progress is a read: it does not award either.
	if _, err := e.svc.Progress(context.Background(), e.user); err != nil {
		t.Fatal(err)
	}
	if n := e.badgeRows(); n != 0 {
		t.Fatalf("Progress awarded %d badges", n)
	}
}

func TestHistoryCountsOnNextEvent(t *testing.T) {
	e := newEnv(t, "UTC")
	e.sessions(57, 12)
	e.sessions(1, 9) // the 58th
	got := e.evaluate(award.Training)
	if want := []string{"workouts_1", "workouts_10", "workouts_50"}; fmt.Sprint(ids(got)) != fmt.Sprint(want) {
		t.Fatalf("earned %v, want %v", ids(got), want)
	}
	for _, g := range got {
		if !g.At.Equal(now) {
			t.Fatalf("%s at %v, want the shared %v", g.Badge.ID, g.At, now)
		}
	}
	// Other areas stay untouched by a training event.
	if n := e.badgeRows(); n != 3 {
		t.Fatalf("badge rows = %d, want 3", n)
	}
}

func TestBadgeOnce(t *testing.T) {
	e := newEnv(t, "UTC")
	e.sessions(10, 12)
	if got := e.evaluate(award.Training); len(got) != 2 {
		t.Fatalf("first: %v", ids(got))
	}
	if got := e.evaluate(award.Training); len(got) != 0 {
		t.Fatalf("second evaluate earned %v", ids(got))
	}
	e.svc.Grant(context.Background(), e.user, now, "workouts_1")
	if n := e.badgeRows(); n != 2 {
		t.Fatalf("badge rows = %d, want 2", n)
	}
}

func TestEarlyBirdUsesLocalTime(t *testing.T) {
	// 06:30 in Lisbon (UTC+1 in summer) is 05:30 UTC: early. 06:30 UTC for a
	// New York user is 02:30 local: also early. 11:30 UTC in New York is 07:30
	// local: not early, though it is before 12:00 UTC.
	cases := []struct {
		tz    string
		start string
		early bool
	}{
		{"Europe/Lisbon", "2026-07-01 05:30+00", true},
		{"Europe/Lisbon", "2026-07-01 06:30+00", false}, // 07:30 local
		{"America/New_York", "2026-07-01 10:30+00", true},
		{"America/New_York", "2026-07-01 11:30+00", false},
		{"", "2026-07-01 06:30+00", true},
	}
	for _, c := range cases {
		e := newEnv(t, c.tz)
		e.exec(`INSERT INTO activity_sessions (user_id, activity_code, status, weight_kg_snapshot, started_at, ended_at)
			SELECT $1, 'running', 'completed', 70, $2::timestamptz + make_interval(days => i), $2::timestamptz + make_interval(days => i, mins => 30)
			FROM generate_series(0, 9) i`, c.start)
		got := false
		for _, x := range e.evaluate(award.Training) {
			got = got || x.Badge.ID == "early_bird"
		}
		if got != c.early {
			t.Errorf("%q at %s: early_bird = %v, want %v", c.tz, c.start, got, c.early)
		}
	}
}

func TestFirstRampIsNotAPR(t *testing.T) {
	e := newEnv(t, "UTC")
	// A new exercise ramped across one session, and a second ramped across
	// another: no record has anything to beat.
	for i, kg := range []float64{40, 60, 80, 100} {
		e.exec(`INSERT INTO set_logs (user_id, log_date, exercise_slug, exercise_name, set_number, weight_kg, reps, kind, performed_at)
			VALUES ($1, '2026-02-01', 'squat', 'Squat', $2, $3, 5, 'work', timestamptz '2026-02-01 10:00+00' + make_interval(mins => $2::int))`, i+1, kg)
	}
	p, err := e.svc.Progress(context.Background(), e.user)
	if err != nil {
		t.Fatal(err)
	}
	if p["pr_1"] != 0 {
		t.Fatalf("pr count = %d after one ramping session, want 0", p["pr_1"])
	}
	// The next session beating it is the first record.
	e.setLog(40, 120, 5, "work")
	e.exec(`UPDATE set_logs SET exercise_slug='squat', exercise_name='Squat' WHERE kind='work' AND weight_kg=120 AND user_id=$1`)
	p, _ = e.svc.Progress(context.Background(), e.user)
	if p["pr_1"] != 1 {
		t.Fatalf("pr count = %d after beating it, want 1", p["pr_1"])
	}
}

func TestPrivateAreaWritesPrivateRow(t *testing.T) {
	e := newEnv(t, "UTC")
	e.exec(`INSERT INTO journal_entries (user_id, content) VALUES ($1, 'x')`)
	e.sessions(1, 12)
	e.evaluate()
	rows, err := e.pool.Query(context.Background(), `SELECT source_key, private, category FROM achievements WHERE user_id=$1 AND kind='badge_earned'`, e.user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var key, cat string
		var private bool
		if err := rows.Scan(&key, &private, &cat); err != nil {
			t.Fatal(err)
		}
		seen[key] = private
		if cat != "awards" {
			t.Errorf("%s category %q", key, cat)
		}
	}
	if private, ok := seen["journal_1"]; !ok || !private {
		t.Errorf("journal_1 private=%v present=%v, want a private row", private, ok)
	}
	if private, ok := seen["workouts_1"]; !ok || private {
		t.Errorf("workouts_1 private=%v present=%v, want a public row", private, ok)
	}
}

func TestStreakFromLongestRun(t *testing.T) {
	e := newEnv(t, "UTC")
	// A 30-day run long ago, a gap, and one check-in today.
	e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy) SELECT $1, date '2025-03-01' + i, 3, 3 FROM generate_series(0, 28) i`)
	e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy) VALUES ($1, '2026-09-30', 3, 3)`)
	got := ids(e.evaluate(award.Consistency))
	if fmt.Sprint(got) != fmt.Sprint([]string{"streak_7"}) {
		t.Fatalf("29-day run: earned %v, want streak_7 only", got)
	}
	e.exec(`INSERT INTO check_ins (user_id, local_date, mood, energy) VALUES ($1, '2025-03-30', 3, 3)`)
	got = ids(e.evaluate(award.Consistency))
	if fmt.Sprint(got) != fmt.Sprint([]string{"streak_30"}) {
		t.Fatalf("30-day run: earned %v, want streak_30", got)
	}
}

func TestProteinNeedsAPlan(t *testing.T) {
	e := newEnv(t, "UTC")
	e.food(1, 12, 500)
	p, err := e.svc.Progress(context.Background(), e.user)
	if err != nil {
		t.Fatal(err)
	}
	if p["protein_10"] != 0 {
		t.Fatalf("protein days = %d with no plan, want 0", p["protein_10"])
	}
}

func TestListenedToBodyIsGrantOnly(t *testing.T) {
	e := newEnv(t, "UTC")
	e.sessions(100, 12)
	e.evaluate()
	p, _ := e.svc.Progress(context.Background(), e.user)
	if p["listened_to_body"] != 0 {
		t.Fatal("listened_to_body earned by Evaluate")
	}
	e.svc.Grant(context.Background(), e.user, now, "listened_to_body")
	p, _ = e.svc.Progress(context.Background(), e.user)
	if p["listened_to_body"] != 1 {
		t.Fatalf("progress after Grant = %d, want 1", p["listened_to_body"])
	}
}

func TestUnseenThenSeen(t *testing.T) {
	e := newEnv(t, "UTC")
	e.sessions(10, 12)
	e.evaluate(award.Training)
	ctx := context.Background()
	un, err := e.svc.Unseen(ctx, e.user)
	if err != nil || len(un) != 2 {
		t.Fatalf("unseen = %v, %v", ids(un), err)
	}
	if err := e.svc.MarkSeen(ctx, e.user, "workouts_1"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.MarkSeen(ctx, e.user, "workouts_1"); err != nil {
		t.Fatal(err)
	}
	un, _ = e.svc.Unseen(ctx, e.user)
	if fmt.Sprint(ids(un)) != "[workouts_10]" {
		t.Fatalf("unseen after seen = %v", ids(un))
	}
	all, _ := e.svc.Earned(ctx, e.user)
	if len(all) != 2 {
		t.Fatalf("earned = %v", ids(all))
	}
}

func TestFailingRuleDoesNotStopOthers(t *testing.T) {
	e := newEnv(t, "UTC")
	e.sessions(1, 12)
	e.exec(`INSERT INTO journal_entries (user_id, content) VALUES ($1, 'x')`)
	e.svc.FailMetric("workouts")
	got, err := e.svc.Evaluate(context.Background(), e.user, now)
	if err == nil {
		t.Fatal("want the joined rule error")
	}
	if fmt.Sprint(ids(got)) != "[journal_1]" {
		t.Fatalf("earned %v, want journal_1 despite the failing workouts rule", ids(got))
	}
}

func TestOneSessionOfRecordsIsOnePR(t *testing.T) {
	e := newEnv(t, "UTC")
	e.setLog(1, 60, 5, "work") // history: the baseline
	for i := 0; i < 4; i++ {   // one later day, four escalating record sets
		e.setLog(5, float64(70+i*10), 5, "work")
	}
	p, err := e.svc.Progress(context.Background(), e.user)
	if err != nil {
		t.Fatal(err)
	}
	if p["pr_1"] != 1 {
		t.Fatalf("pr count = %d, want 1", p["pr_1"])
	}
}
