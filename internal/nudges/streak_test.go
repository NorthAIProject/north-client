package nudges_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// streakUser is an account past its first week with a three-day check-in
// streak that ended yesterday, relative to the frozen clock. Nothing today.
func streakUser(t *testing.T, pool *pgxpool.Pool, email string, now time.Time) users.User {
	t.Helper()
	user := mustOnboard(t, pool, seedUser(t, pool, email), now.AddDate(0, 0, -30))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for back := 1; back <= 3; back++ {
		writeCheckIn(t, pool, user.ID, today.AddDate(0, 0, -back))
	}
	return user
}

func openKinds(t *testing.T, svc *nudges.Service, user users.User) []string {
	t.Helper()
	list, err := svc.ListOpen(context.Background(), user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(list))
	for _, n := range list {
		kinds = append(kinds, n.Kind)
	}
	return kinds
}

// The missed-check-in rule waits two quiet days and so always arrives after
// the streak is gone. This one arrives the evening before, which is the only
// time a warning about loss can do anything.
func TestEvaluateWarnsTheEveningAStreakWouldBreak(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	evening := time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC)
	user := streakUser(t, pool, "streak-evening@north.test", evening)
	svc := evalService(pool, evening)

	n, err := svc.Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1 (open: %v)", n, openKinds(t, svc, user))
	}
	list, err := svc.ListOpen(ctx, user.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != nudges.KindStreakAtRisk {
		t.Fatalf("open = %#v", list)
	}
	if !strings.Contains(list[0].Title, "3-day streak") || list[0].Href != "/app/check-ins" {
		t.Fatalf("nudge = %q → %q", list[0].Title, list[0].Href)
	}

	// The next sweep that evening is a no-op: one warning per local day.
	n, err = evalService(pool, evening.Add(time.Hour)).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second sweep created %d, want 0", n)
	}
}

func TestStreakWarningWaitsForTheEvening(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	morning := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	user := streakUser(t, pool, "streak-morning@north.test", morning)

	n, err := evalService(pool, morning).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d at 10:00, want 0 (open: %v)", n, openKinds(t, evalService(pool, morning), user))
	}
}

func TestStreakWarningNeedsAStreakWorthKeeping(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	evening := time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "streak-short@north.test"), evening.AddDate(0, 0, -30))
	// Two days, not three.
	writeCheckIn(t, pool, user.ID, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))

	n, err := evalService(pool, evening).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d for a 2-day streak, want 0", n)
	}
}

func TestStreakWarningIsSilentOnceTodayIsLogged(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	evening := time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC)
	user := streakUser(t, pool, "streak-done@north.test", evening)
	writeCheckIn(t, pool, user.ID, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))

	n, err := evalService(pool, evening).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d after today's check-in, want 0", n)
	}
}

// The same switch that silences "you stopped checking in" silences "you are
// about to". One preference, both directions.
func TestStreakWarningRespectsTheMissedCheckInSwitch(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	evening := time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC)
	user := streakUser(t, pool, "streak-off@north.test", evening)

	prefs := notifications.NewService(notifications.NewRepository(pool))
	if _, err := prefs.Upsert(ctx, user.ID, notifications.Input{
		NudgeMissedCheckIn: false,
		NudgeGoalDeadline:  true,
		CoachActivity:      true,
		TrainingReminders:  true,
		QuietStart:         "22:00",
		QuietEnd:           "07:00",
	}); err != nil {
		t.Fatal(err)
	}

	n, err := evalService(pool, evening).WithPrefs(prefs).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d with the switch off, want 0", n)
	}
}
