package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// prefsService is evalService plus the notification settings, which is how
// both mains build it.
func prefsService(pool *pgxpool.Pool, now time.Time) *nudges.Service {
	return evalService(pool, now).
		WithPrefs(notifications.NewService(notifications.NewRepository(pool)))
}

func savePrefs(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, in notifications.Input) {
	t.Helper()
	svc := notifications.NewService(notifications.NewRepository(pool))
	if _, err := svc.Upsert(context.Background(), userID, in); err != nil {
		t.Fatal(err)
	}
}

// seedUserIn registers an account that lives somewhere other than UTC.
func seedUserIn(t *testing.T, pool *pgxpool.Pool, email, zone string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     zone,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestEvaluateSkipsASwitchedOffKind(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	user := mustOnboard(t, pool, seedUser(t, pool, "prefs-off@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

	savePrefs(t, pool, user.ID, notifications.Input{
		NudgeMissedCheckIn: false,
		NudgeGoalDeadline:  true,
	})

	n, err := prefsService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want 0 — the missed check-in nudge is switched off", n)
	}
}

func TestEvaluateStillNudgesWhenTheKindIsOn(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	user := mustOnboard(t, pool, seedUser(t, pool, "prefs-on@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

	savePrefs(t, pool, user.ID, notifications.Input{
		NudgeMissedCheckIn: true,
		NudgeGoalDeadline:  true,
	})

	n, err := prefsService(pool, now).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d, want 1", n)
	}
}

// Quiet hours defer rather than suppress: the same sweep, run again after the
// window closes, raises exactly what it would have raised before.
func TestQuietHoursDeferRatherThanSuppress(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	user := mustOnboard(t, pool, seedUser(t, pool, "prefs-quiet@north.test"), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

	savePrefs(t, pool, user.ID, notifications.Input{
		NudgeMissedCheckIn: true,
		NudgeGoalDeadline:  true,
		QuietHoursEnabled:  true,
		QuietStart:         "22:00",
		QuietEnd:           "07:00",
	})

	inside := time.Date(2026, 8, 15, 23, 30, 0, 0, time.UTC)
	n, err := prefsService(pool, inside).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d during quiet hours, want 0", n)
	}

	after := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	n, err = prefsService(pool, after).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("created = %d outside quiet hours, want 1", n)
	}
}

// Quiet hours are read in the user's own clock, not the server's.
func TestQuietHoursFollowTheUsersTimezone(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	user := mustOnboard(t, pool,
		seedUserIn(t, pool, "prefs-tz@north.test", "Pacific/Auckland"),
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

	savePrefs(t, pool, user.ID, notifications.Input{
		NudgeMissedCheckIn: true,
		NudgeGoalDeadline:  true,
		QuietHoursEnabled:  true,
		QuietStart:         "22:00",
		QuietEnd:           "07:00",
	})

	// 12:00 UTC on 15 Aug 2026 is midnight in Auckland — inside the window,
	// even though the server clock says the middle of the day.
	noonUTC := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	n, err := prefsService(pool, noonUTC).Evaluate(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created = %d, want 0 — it is midnight where this person lives", n)
	}
}
