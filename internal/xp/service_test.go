package xp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/xp"
)

// now is a Wednesday; the week began Monday 2026-09-28.
var now = time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)

func person(t *testing.T, pool *pgxpool.Pool, email, name string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: name, Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// follow makes follower follow followee, accepted.
func follow(t *testing.T, svc *social.Service, follower, followee users.User, handle string) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.SetHandle(ctx, followee.ID, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Follow(ctx, follower.ID, handle); err != nil {
		t.Fatal(err)
	}
	if err := svc.Accept(ctx, followee.ID, follower.ID); err != nil {
		t.Fatal(err)
	}
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// session adds a completed session ending at end, lasting minutes.
func session(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, end time.Time, minutes int) {
	t.Helper()
	exec(t, pool, `INSERT INTO activity_sessions (user_id, activity_code, status, weight_kg_snapshot, started_at, ended_at)
		VALUES ($1, 'running', 'completed', 70, $2, $3)`, userID, end.Add(-time.Duration(minutes)*time.Minute), end)
}

func newService(pool *pgxpool.Pool) *xp.Service {
	goalSvc := goals.NewService(goals.NewRepository(pool))
	return xp.NewService(pool, checkins.NewService(checkins.NewRepository(pool), goalSvc))
}

// Every rule in the table, counted once, in the week and all time.
func TestSummaryCountsOnlyWhatTheRulesPay(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@example.com", "Ana")

	// Three sessions on Tuesday: two pay, the third is over the day's cap.
	// A five-minute one never pays.
	tue := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	session(t, pool, ana.ID, tue, 30)
	session(t, pool, ana.ID, tue.Add(2*time.Hour), 30)
	session(t, pool, ana.ID, tue.Add(4*time.Hour), 30)
	session(t, pool, ana.ID, tue.Add(6*time.Hour), 5)
	// Last week's session pays all time only.
	session(t, pool, ana.ID, tue.AddDate(0, 0, -7), 40)

	// A habit scheduled Mondays (1) and Tuesdays (2): kept both, and once on
	// Wednesday, which is not its day.
	var habitID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO habits (user_id, name, days_of_week) VALUES ($1, 'Stretch', '{1,2}') RETURNING id`,
		ana.ID).Scan(&habitID); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-09-28", "2026-09-29", "2026-09-30"} {
		exec(t, pool, `INSERT INTO habit_completions (habit_id, user_id, local_date) VALUES ($1, $2, $3)`, habitID, ana.ID, d)
	}

	// Check-ins Saturday to Wednesday, five in a row. Days 3 to 5 of the run
	// are streak days, and all three (Monday to Wednesday) are this week: the
	// run is counted from before the week began.
	for _, d := range []string{"2026-09-26", "2026-09-27", "2026-09-28", "2026-09-29", "2026-09-30"} {
		exec(t, pool, `INSERT INTO check_ins (user_id, local_date, mood, energy) VALUES ($1, $2, 3, 3)`, ana.ID, d)
	}

	// One goal achieved this week with one milestone; one goal achieved and
	// then reopened, which pays nothing.
	var goalID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO goals (user_id, title, status, closed_at) VALUES ($1, 'Run 10k', 'achieved', $2) RETURNING id`,
		ana.ID, tue).Scan(&goalID); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `INSERT INTO goal_milestones (goal_id, user_id, title, status, completed_at) VALUES ($1, $2, '5k', 'completed', $3)`,
		goalID, ana.ID, tue)
	exec(t, pool, `INSERT INTO goals (user_id, title, status, closed_at) VALUES ($1, 'Reopened', 'active', NULL)`, ana.ID)

	s, err := newService(pool).Summary(ctx, ana, now)
	if err != nil {
		t.Fatal(err)
	}
	week := map[string]int{}
	for _, e := range s.Week {
		week[e.Kind] = e.Count
	}
	want := map[string]int{xp.KindWorkout: 2, xp.KindHabitKept: 2, xp.KindStreakDay: 3, xp.KindMilestone: 1, xp.KindGoal: 1}
	for kind, n := range want {
		if week[kind] != n {
			t.Errorf("week %s = %d, want %d", kind, week[kind], n)
		}
	}
	wantWeek := 2*xp.PointsWorkout + 2*xp.PointsHabitKept + 3*xp.PointsStreakDay + xp.PointsMilestone + xp.PointsGoal
	if s.WeekTotal != wantWeek {
		t.Errorf("week total = %d, want %d", s.WeekTotal, wantWeek)
	}
	if s.Total != wantWeek+xp.PointsWorkout {
		t.Errorf("total = %d, want %d (the week plus last week's session)", s.Total, wantWeek+xp.PointsWorkout)
	}
	if s.Level.Title != "Mover" {
		t.Errorf("level = %s, want Mover at %d XP", s.Level.Title, s.Total)
	}
}

// The board holds the viewer and the friends who share that metric; somebody
// who does not share, or is blocked, is not on it.
func TestBoardShowsOnlyFriendsWhoShare(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	ctx := context.Background()
	socialSvc := social.NewService(social.NewRepository(pool))
	sharing := achievements.NewService(pool)

	ana := person(t, pool, "ana@example.com", "Ana")
	bo := person(t, pool, "bo@example.com", "Bo")
	cleo := person(t, pool, "cleo@example.com", "Cleo")
	dan := person(t, pool, "dan@example.com", "Dan")
	follow(t, socialSvc, ana, bo, "bo_lifts")
	follow(t, socialSvc, ana, cleo, "cleo_runs")
	follow(t, socialSvc, ana, dan, "dan_rows")
	for _, u := range []users.User{bo, dan} {
		if _, err := sharing.SetSharing(ctx, u.ID, achievements.Sharing{XP: true}); err != nil {
			t.Fatal(err)
		}
	}
	// Cleo shares nothing; Dan shares but blocks Ana.
	if err := socialSvc.Block(ctx, dan.ID, ana.ID); err != nil {
		t.Fatal(err)
	}

	tue := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	session(t, pool, bo.ID, tue, 30)
	session(t, pool, cleo.ID, tue, 30)

	b, err := newService(pool).Board(ctx, ana, xp.MetricXP, xp.PeriodWeek, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Entries) != 2 || b.Entries[0].DisplayName != "Bo" || b.Entries[0].Value != xp.PointsWorkout ||
		b.Entries[1].DisplayName != "Ana" || !b.Entries[1].Me || b.Entries[1].Rank != 2 {
		t.Fatalf("board = %+v, want Bo %d XP then Ana", b.Entries, xp.PointsWorkout)
	}
	if b.Sharing {
		t.Error("Ana shares nothing, so Sharing should be false")
	}
	if b.Entries[0].Level == nil {
		t.Error("an XP board carries levels")
	}

	// Streaks need the streaks switch, which nobody turned on.
	b, err = newService(pool).Board(ctx, ana, xp.MetricStreak, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Entries) != 1 || !b.Entries[0].Me {
		t.Fatalf("streak board = %+v, want Ana alone", b.Entries)
	}

	if _, err = newService(pool).Board(ctx, ana, "calories", "", now); err == nil {
		t.Error("an unknown metric should be refused")
	}
}
