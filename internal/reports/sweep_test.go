package reports_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// sweepFixture wires the sweep the way cmd/worker does, over a real database.
func sweepFixture(t *testing.T, now time.Time) (*reports.Sweeper, *reports.Service, *users.Service, *notifications.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)

	userSvc := users.NewService(users.NewRepository(pool))
	notifSvc := notifications.NewService(notifications.NewRepository(pool))
	svc := reports.NewService(reports.Options{
		Repository: reports.NewRepository(pool),
		Users:      userSvc,
		Queue:      &stubQueue{},
		Now:        func() time.Time { return now },
	})

	sweeper := reports.NewSweeper(svc, userSvc, notifSvc, nil).
		WithClock(func() time.Time { return now })

	return sweeper, svc, userSvc, notifSvc, pool
}

func onboardedUser(t *testing.T, pool *pgxpool.Pool, userSvc *users.Service, email, zone string) users.User {
	t.Helper()
	ctx := context.Background()

	u, err := userSvc.Register(ctx, users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     zone,
	})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	if _, err := userSvc.MarkOnboarded(ctx, u.ID); err != nil {
		t.Fatalf("onboard %s: %v", email, err)
	}
	return u
}

func optIn(t *testing.T, notifSvc *notifications.Service, userID uuid.UUID) {
	t.Helper()
	if _, err := notifSvc.Upsert(context.Background(), userID, notifications.Input{
		NudgeMissedCheckIn: true,
		NudgeGoalDeadline:  true,
		WeeklyReportAuto:   true,
	}); err != nil {
		t.Fatal(err)
	}
}

// Monday 07:00 in Lisbon. The review written is of the week that just closed,
// not the one that started this morning.
func TestSweepGeneratesTheWeekThatJustClosed(t *testing.T) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC) // 07:00 in Lisbon (UTC+1)
	sweeper, svc, userSvc, notifSvc, pool := sweepFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "sweep-lisbon@north.test", "Europe/Lisbon")
	optIn(t, notifSvc, user.ID)

	if err := sweeper.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	list, err := svc.List(ctx, user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("reports = %d, want 1", len(list))
	}

	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	if got := list[0].PeriodStart; got.Year() != wantStart.Year() || got.Month() != wantStart.Month() || got.Day() != wantStart.Day() {
		t.Fatalf("period start = %s, want %s", got.Format("2006-01-02"), wantStart.Format("2006-01-02"))
	}
}

// The sweep runs hourly all Monday. It must not write the same week twice.
func TestSweepIsIdempotentAcrossRuns(t *testing.T) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	sweeper, svc, userSvc, notifSvc, pool := sweepFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "sweep-twice@north.test", "Europe/Lisbon")
	optIn(t, notifSvc, user.ID)

	for i := range 3 {
		if err := sweeper.HandleSweep(ctx, nil); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}

	list, err := svc.List(ctx, user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("reports = %d after three sweeps, want 1", len(list))
	}
}

func TestSweepSkipsAccountsThatDidNotAsk(t *testing.T) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	sweeper, svc, userSvc, _, pool := sweepFixture(t, now)
	ctx := context.Background()

	// No preferences row at all — the defaults leave this off.
	user := onboardedUser(t, pool, userSvc, "sweep-optout@north.test", "Europe/Lisbon")

	if err := sweeper.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	list, err := svc.List(ctx, user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("reports = %d for an account that never opted in, want 0", len(list))
	}
}

// The same instant is Monday morning in one place and Sunday evening in
// another. Only the first should get a review.
func TestSweepWaitsForEachTimezonesMonday(t *testing.T) {
	// Monday 08:00 in Auckland (NZST, UTC+12) is Sunday 20:00 UTC and
	// Sunday 13:00 in Los Angeles.
	now := time.Date(2026, 8, 16, 20, 0, 0, 0, time.UTC)
	sweeper, svc, userSvc, notifSvc, pool := sweepFixture(t, now)
	ctx := context.Background()

	auckland := onboardedUser(t, pool, userSvc, "sweep-akl@north.test", "Pacific/Auckland")
	losAngeles := onboardedUser(t, pool, userSvc, "sweep-lax@north.test", "America/Los_Angeles")
	optIn(t, notifSvc, auckland.ID)
	optIn(t, notifSvc, losAngeles.ID)

	if err := sweeper.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	aklReports, err := svc.List(ctx, auckland.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(aklReports) != 1 {
		t.Fatalf("Auckland reports = %d, want 1 — it is Monday morning there", len(aklReports))
	}

	laxReports, err := svc.List(ctx, losAngeles.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(laxReports) != 0 {
		t.Fatalf("Los Angeles reports = %d, want 0 — it is still Sunday there", len(laxReports))
	}
}

// Before the local morning hour, nothing happens even on the right day.
func TestSweepWaitsForTheMorning(t *testing.T) {
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC) // 02:00 in Lisbon
	sweeper, svc, userSvc, notifSvc, pool := sweepFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "sweep-early@north.test", "Europe/Lisbon")
	optIn(t, notifSvc, user.ID)

	if err := sweeper.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	list, err := svc.List(ctx, user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("reports = %d at 02:00 local, want 0", len(list))
	}
}
