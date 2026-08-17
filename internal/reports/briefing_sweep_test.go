package reports_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/reports"
	"github.com/NorthAIProject/north-client/internal/users"
)

func briefingFixture(t *testing.T, now time.Time) (*reports.BriefingSweeper, *reports.Service, *users.Service, *notifications.Service, *pgxpool.Pool) {
	t.Helper()
	_, svc, userSvc, notifSvc, pool := sweepFixture(t, now)

	briefer := reports.NewBriefingSweeper(svc, userSvc, notifSvc, nil).
		WithClock(func() time.Time { return now })

	return briefer, svc, userSvc, notifSvc, pool
}

func optInBriefing(t *testing.T, notifSvc *notifications.Service, userID uuid.UUID) {
	t.Helper()
	if _, err := notifSvc.Upsert(context.Background(), userID, notifications.Input{
		NudgeMissedCheckIn: true,
		NudgeGoalDeadline:  true,
		DailyBriefingAuto:  true,
	}); err != nil {
		t.Fatal(err)
	}
}

// The briefing covers today, in the reader's own timezone.
func TestBriefingSweepWritesToday(t *testing.T) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC) // 07:00 in Lisbon
	briefer, svc, userSvc, notifSvc, pool := briefingFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "briefing-today@north.test", "Europe/Lisbon")
	optInBriefing(t, notifSvc, user.ID)

	if err := briefer.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListKind(ctx, user.ID, reports.KindDaily, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("briefings = %d, want 1", len(list))
	}
	if list[0].Kind != reports.KindDaily {
		t.Fatalf("kind = %q, want daily", list[0].Kind)
	}

	want := time.Date(2026, 8, 17, 0, 0, 0, 0, loc)
	if g := list[0].PeriodStart; g.Year() != want.Year() || g.Month() != want.Month() || g.Day() != want.Day() {
		t.Fatalf("period start = %s, want %s", g.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}

// Hourly all day, but one briefing per morning.
func TestBriefingSweepIsIdempotentAcrossRuns(t *testing.T) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	briefer, svc, userSvc, notifSvc, pool := briefingFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "briefing-twice@north.test", "Europe/Lisbon")
	optInBriefing(t, notifSvc, user.ID)

	for i := range 3 {
		if err := briefer.HandleSweep(ctx, nil); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}

	list, err := svc.ListKind(ctx, user.ID, reports.KindDaily, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("briefings = %d after three sweeps, want 1", len(list))
	}
}

// Opting out of the briefing must not be satisfied by the weekly review's flag.
func TestBriefingSweepSkipsAccountsThatDidNotOptIn(t *testing.T) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	briefer, svc, userSvc, notifSvc, pool := briefingFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "briefing-optout@north.test", "Europe/Lisbon")
	// Weekly review on, briefing off.
	optIn(t, notifSvc, user.ID)

	if err := briefer.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	list, err := svc.ListKind(ctx, user.ID, reports.KindDaily, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("briefings = %d for an account that did not ask, want 0", len(list))
	}
}

// Before the local briefing hour nothing is written, even though the sweep runs.
func TestBriefingSweepWaitsForLocalMorning(t *testing.T) {
	// 02:00 in Lisbon — before briefingHour.
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	briefer, svc, userSvc, notifSvc, pool := briefingFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "briefing-early@north.test", "Europe/Lisbon")
	optInBriefing(t, notifSvc, user.ID)

	if err := briefer.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	list, err := svc.ListKind(ctx, user.ID, reports.KindDaily, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("briefings = %d before the local morning, want 0", len(list))
	}
}

// The weekly review list must not fill up with briefings.
func TestWeeklyListExcludesBriefings(t *testing.T) {
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	briefer, svc, userSvc, notifSvc, pool := briefingFixture(t, now)
	ctx := context.Background()

	user := onboardedUser(t, pool, userSvc, "briefing-separate@north.test", "Europe/Lisbon")
	optInBriefing(t, notifSvc, user.ID)

	if err := briefer.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := svc.RequestGenerate(ctx, user.ID, time.Time{}); err != nil {
		t.Fatalf("request weekly: %v", err)
	}

	weekly, err := svc.List(ctx, user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range weekly {
		if r.Kind != reports.KindWeekly {
			t.Fatalf("weekly list contains a %q report", r.Kind)
		}
	}
	if len(weekly) != 1 {
		t.Fatalf("weekly reports = %d, want 1", len(weekly))
	}
}
