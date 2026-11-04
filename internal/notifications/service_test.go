package notifications_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, DisplayName: "Test", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// An account that has never opened the settings page has preferences all the
// same — a missing row is unconfigured, not an error.
func TestGetReturnsDefaultsWithoutARow(t *testing.T) {
	pool := testdb.New(t)
	svc := notifications.NewService(notifications.NewRepository(pool))
	user := seedUser(t, pool, "prefs-none@north.test")

	got, err := svc.Get(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("user id = %s, want %s", got.UserID, user.ID)
	}
	if !got.NudgeMissedCheckIn || !got.NudgeGoalDeadline || got.WeeklyReportAuto {
		t.Fatalf("defaults = %+v", got)
	}
}

// The read and the claim have to agree on what a window is, including for a
// period that starts at local midnight far from UTC. If they did not, the sweep
// would compute a digest it had already sent, or skip one it had not.
func TestDigestClaimedSeesWhatClaimDigestBooked(t *testing.T) {
	pool := testdb.New(t)
	repo := notifications.NewRepository(pool)
	user := seedUser(t, pool, "digest-claimed@north.test")
	ctx := context.Background()

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	period := time.Date(2026, 9, 14, 0, 0, 0, 0, tokyo)

	assertClaimed := func(cadence string, at time.Time, want bool) {
		t.Helper()
		got, claimErr := repo.DigestClaimed(ctx, user.ID, cadence, at)
		if claimErr != nil {
			t.Fatalf("digest claimed: %v", claimErr)
		}
		if got != want {
			t.Errorf("DigestClaimed(%s, %s) = %v, want %v", cadence, at.Format(time.DateOnly), got, want)
		}
	}

	assertClaimed(notifications.CadenceWeekly, period, false)

	claimed, err := repo.ClaimDigest(ctx, user.ID, notifications.CadenceWeekly, period)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed {
		t.Fatal("the first claim of a window lost")
	}

	assertClaimed(notifications.CadenceWeekly, period, true)
	assertClaimed(notifications.CadenceDaily, period, false)
	assertClaimed(notifications.CadenceWeekly, period.AddDate(0, 0, 7), false)
}

func TestUpsertRoundTrips(t *testing.T) {
	pool := testdb.New(t)
	svc := notifications.NewService(notifications.NewRepository(pool))
	ctx := context.Background()
	user := seedUser(t, pool, "prefs-save@north.test")

	saved, err := svc.Upsert(ctx, user.ID, notifications.Input{
		NudgeMissedCheckIn: false,
		NudgeGoalDeadline:  true,
		WeeklyReportAuto:   true,
		QuietHoursEnabled:  true,
		QuietStart:         "23:30",
		QuietEnd:           "06:15",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.NudgeMissedCheckIn || !saved.WeeklyReportAuto || saved.QuietStart != "23:30" {
		t.Fatalf("saved = %+v", saved)
	}

	// Saving twice must update the one row rather than conflict on user_id.
	again, err := svc.Upsert(ctx, user.ID, notifications.Input{NudgeGoalDeadline: true})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if again.QuietHoursEnabled {
		t.Error("quiet hours survived a save that switched them off")
	}

	got, err := svc.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NudgeGoalDeadline != true || got.NudgeMissedCheckIn != false {
		t.Fatalf("reloaded = %+v", got)
	}
}
