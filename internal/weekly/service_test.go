package weekly_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/weekly"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// Sunday 4 October 2026, evening: the review plans the week of 5 October.
var sunday = time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC)

type fakeReports struct {
	list    []reports.Report
	ensured int
}

func (f *fakeReports) ListKind(context.Context, uuid.UUID, reports.Kind, bool) ([]reports.Report, error) {
	return f.list, nil
}

func (f *fakeReports) EnsureWeekly(_ context.Context, userID uuid.UUID, w reports.Week) (reports.Report, bool, error) {
	f.ensured++
	return reports.Report{ID: uuid.New(), UserID: userID, PeriodStart: w.Start, Title: w.Title(), Status: reports.StatusPending}, true, nil
}

type recorded struct{ weeks []time.Time }

func (r *recorded) WeekReviewed(_ context.Context, _ uuid.UUID, weekStart, _ time.Time) {
	r.weeks = append(r.weeks, weekStart)
}

func setup(t *testing.T) (*pgxpool.Pool, users.User, *goals.Service) {
	t.Helper()
	pool := testdb.New(t)
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: "weekly@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Ana", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return pool, u, goals.NewService(goals.NewRepository(pool))
}

func TestPlanningWeek(t *testing.T) {
	cases := map[string]struct {
		at   time.Time
		want string
	}{
		"monday plans this week":   {time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), "2026-09-28"},
		"thursday plans this week": {time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), "2026-09-28"},
		"friday plans next week":   {time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), "2026-10-05"},
		"sunday plans next week":   {sunday, "2026-10-05"},
	}
	for name, c := range cases {
		if got := weekly.PlanningWeek(c.at, time.UTC).Format(time.DateOnly); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

// The loop: a review sets next week's priorities, goal order and volume;
// the goals list follows the order and next week trains at that volume.
func TestSetFocusClosesTheLoop(t *testing.T) {
	ctx := context.Background()
	pool, user, goalSvc := setup(t)
	run, _ := goalSvc.Create(ctx, user.ID, goals.Input{Title: "Run a half marathon", Category: "fitness"})
	deck, _ := goalSvc.Create(ctx, user.ID, goals.Input{Title: "Ship the deck", Category: "work"})
	rep := &fakeReports{}
	ach := &recorded{}
	svc := weekly.NewService(pool, goalSvc, rep).WithAchievements(ach).WithClock(func() time.Time { return sunday })

	rv, err := svc.Review(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if rv.Planning.Format(time.DateOnly) != "2026-10-05" || rv.Reviewing.Format(time.DateOnly) != "2026-09-28" {
		t.Fatalf("weeks = %s / %s", rv.Reviewing, rv.Planning)
	}
	if rep.ensured != 1 || rv.Report == nil || rv.Report.Ready {
		t.Fatalf("the week's report was not asked for: ensured %d, %+v", rep.ensured, rv.Report)
	}

	if _, err = svc.SetFocus(ctx, user, weekly.Input{
		Priorities: []string{" Three runs ", "", "Ship the deck"},
		GoalOrder:  []uuid.UUID{deck.ID, run.ID},
		Volume:     plan.VolumeDeload,
	}); err != nil {
		t.Fatal(err)
	}

	active, _ := goalSvc.ListActive(ctx, user.ID)
	if len(active) != 2 || active[0].ID != deck.ID || active[0].Priority != 1 || active[1].Priority != 2 {
		t.Fatalf("goal order = %+v", active)
	}
	if v, _ := svc.VolumeFor(ctx, user, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)); v != plan.VolumeDeload {
		t.Fatalf("next week's volume = %q", v)
	}
	if v, _ := svc.VolumeFor(ctx, user, sunday); v != plan.VolumeHold {
		t.Fatalf("this week's volume = %q, want hold", v)
	}
	if len(ach.weeks) != 1 {
		t.Fatalf("achievement recorded %d times", len(ach.weeks))
	}

	again, _ := svc.Review(ctx, user)
	if again.Current == nil || len(again.Current.Priorities) != 2 || again.Current.Priorities[0] != "Three runs" {
		t.Fatalf("current focus = %+v", again.Current)
	}
	if ok, _ := svc.Reviewed(ctx, user, again.Planning); !ok {
		t.Fatal("the planned week does not count as reviewed")
	}
}

func TestSetFocusRejects(t *testing.T) {
	ctx := context.Background()
	pool, user, goalSvc := setup(t)
	svc := weekly.NewService(pool, goalSvc, &fakeReports{}).WithClock(func() time.Time { return sunday })

	cases := map[string]weekly.Input{
		"four priorities":     {Priorities: []string{"a", "b", "c", "d"}},
		"unknown volume":      {Volume: "max"},
		"someone else's goal": {GoalOrder: []uuid.UUID{uuid.New()}},
	}
	for name, in := range cases {
		if _, err := svc.SetFocus(ctx, user, in); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}
