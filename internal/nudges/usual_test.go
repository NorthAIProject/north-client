package nudges_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// writeCheckInAt files a check-in for a day and says it was saved at a time
// of that day, the way a person checking in at breakfast leaves a trail.
func writeCheckInAt(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, at time.Time) {
	t.Helper()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	writeCheckIn(t, pool, userID, day)
	if _, err := pool.Exec(context.Background(),
		`UPDATE check_ins SET created_at = $3 WHERE user_id = $1 AND local_date = $2`, userID, day, at); err != nil {
		t.Fatal(err)
	}
}

// habitUser checks in around the given time of day on each of the days back
// before now (1 is yesterday).
func habitUser(t *testing.T, pool *pgxpool.Pool, email string, now time.Time, hour, minute int, days ...int) users.User {
	t.Helper()
	user := mustOnboard(t, pool, seedUser(t, pool, email), now.AddDate(0, 0, -40))
	for i, back := range days {
		d := now.AddDate(0, 0, -back)
		writeCheckInAt(t, pool, user.ID, time.Date(d.Year(), d.Month(), d.Day(), hour, minute+i%3*5, 0, 0, time.UTC))
	}
	return user
}

func at(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.UTC)
}

var thursday = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

// Somebody who checks in around eight is asked once, after the time they
// usually would have, and not before.
func TestUsualTimeAsksAfterTheUsualHour(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := habitUser(t, pool, "usual@north.test", thursday, 8, 0, 1, 2, 3, 4, 5, 6)

	if n, err := evalService(pool, at(thursday, 8, 15)).Evaluate(ctx, user); err != nil || n != 0 {
		t.Fatalf("08:15: created %d, err %v; want nothing before the usual time has passed", n, err)
	}
	svc := evalService(pool, at(thursday, 9, 0))
	if n, err := svc.Evaluate(ctx, user); err != nil || n != 1 {
		t.Fatalf("09:00: created %d, err %v; want 1 (open: %v)", n, err, openKinds(t, svc, user))
	}
	list, _ := svc.ListOpen(ctx, user.ID, 10)
	if len(list) != 1 || list[0].Kind != nudges.KindUsualTime || list[0].Href != "/app/check-ins" {
		t.Fatalf("open = %#v", list)
	}
	if nudges.PushCategory(nudges.KindUsualTime) != nudges.CategoryCheckIn {
		t.Fatal("the reminder should carry the check-in buttons")
	}
	if n, _ := evalService(pool, at(thursday, 10, 0)).Evaluate(ctx, user); n != 0 {
		t.Fatalf("second sweep created %d, want 0", n)
	}
}

func TestUsualTimeStaysQuiet(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		days  []int
		now   time.Time
		today bool
		off   bool
	}{
		{name: "fewer than five days to learn from", days: []int{1, 2, 3, 4}, now: at(thursday, 9, 0)},
		{name: "already checked in today", days: []int{1, 2, 3, 4, 5}, now: at(thursday, 9, 0), today: true},
		{name: "window closed", days: []int{1, 2, 3, 4, 5}, now: at(thursday, 11, 30)},
		// After two quiet days the missed-check-in note speaks instead.
		{name: "quiet for three days", days: []int{3, 4, 5, 6, 7}, now: at(thursday, 9, 0)},
		{name: "switched off", days: []int{1, 2, 3, 4, 5}, now: at(thursday, 9, 0), off: true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := testdb.New(t)
			user := habitUser(t, pool, "quiet-"+string(rune('a'+i))+"@north.test", tc.now, 8, 0, tc.days...)
			if tc.today {
				writeCheckIn(t, pool, user.ID, thursday)
			}
			svc := evalService(pool, tc.now)
			if tc.off {
				prefs := notifications.NewService(notifications.NewRepository(pool))
				if _, err := prefs.Upsert(ctx, user.ID, notifications.Input{
					NudgeMissedCheckIn: false, CoachActivity: true, QuietStart: "22:00", QuietEnd: "07:00",
				}); err != nil {
					t.Fatal(err)
				}
				svc = svc.WithPrefs(prefs)
			}
			if _, err := svc.Evaluate(ctx, user); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(openKinds(t, svc, user), nudges.KindUsualTime) {
				t.Fatal("unexpected usual-time reminder")
			}
		})
	}
}

// Check-ins the reminder itself prompted do not teach it a later hour, or it
// would chase its own answers later every week.
func TestUsualTimeIgnoresDaysItAskedOn(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := habitUser(t, pool, "drift@north.test", thursday, 8, 0, 1, 2, 3, 4, 5)
	repo := nudges.NewRepository(pool)
	for back := 6; back <= 10; back++ {
		d := thursday.AddDate(0, 0, -back)
		writeCheckInAt(t, pool, user.ID, at(d, 10, 0))
		if _, _, err := repo.Insert(ctx, user.ID, nudges.Draft{
			Kind: nudges.KindUsualTime, DedupeKey: d.Format("2006-01-02"), Title: "t", Body: "b", Href: "/app/check-ins",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Counting the answered days, the median would be 10:00 and nothing would
	// be due at nine.
	if n, err := evalService(pool, at(thursday, 9, 0)).Evaluate(ctx, user); err != nil || n != 1 {
		t.Fatalf("created %d, err %v; want today's reminder at the person's own hour", n, err)
	}
}

// One check-in ask a day. Once the reminder has asked, the evening warning
// does not ask again.
func TestOneCheckInAskADay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := habitUser(t, pool, "one-ask@north.test", thursday, 8, 0, 1, 2, 3, 4, 5)

	if n, _ := evalService(pool, at(thursday, 9, 0)).Evaluate(ctx, user); n != 1 {
		t.Fatalf("morning created %d, want the reminder", n)
	}
	svc := evalService(pool, at(thursday, 19, 0))
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if kinds := openKinds(t, svc, user); slices.Contains(kinds, nudges.KindStreakAtRisk) {
		t.Fatalf("open = %v; the evening warning should stand down", kinds)
	}
}

// When the person usually checks in in the evening, the gentle reminder goes
// first and the warning waits rather than racing it.
func TestTheWarningWaitsForAnEveningReminder(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := habitUser(t, pool, "evening-habit@north.test", thursday, 19, 0, 1, 2, 3, 4, 5)

	svc := evalService(pool, at(thursday, 18, 10))
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if kinds := openKinds(t, svc, user); len(kinds) != 0 {
		t.Fatalf("18:10 open = %v, want nothing yet", kinds)
	}
	svc = evalService(pool, at(thursday, 19, 45))
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if kinds := openKinds(t, svc, user); len(kinds) != 1 || kinds[0] != nudges.KindUsualTime {
		t.Fatalf("19:45 open = %v, want only the reminder", kinds)
	}
}

// Loss-framed copy stops at 21:00.
func TestStreakWarningEndsAtNine(t *testing.T) {
	pool := testdb.New(t)
	late := time.Date(2026, 9, 2, 21, 15, 0, 0, time.UTC)
	user := streakUser(t, pool, "streak-late@north.test", late)
	svc := evalService(pool, late)
	if _, err := svc.Evaluate(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(openKinds(t, svc, user), nudges.KindStreakAtRisk) {
		t.Fatal("no streak warning after 21:00")
	}
}

// A person holding a rest day does not need to be scared into the app.
func TestStreakWarningStandsDownWhileARestIsHeld(t *testing.T) {
	pool := testdb.New(t)
	evening := time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC)
	user := mustOnboard(t, pool, seedUser(t, pool, "rest-held@north.test"), evening.AddDate(0, 0, -30))
	today := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	for back := 1; back <= 8; back++ {
		writeCheckIn(t, pool, user.ID, today.AddDate(0, 0, -back))
	}
	svc := evalService(pool, evening)
	if _, err := svc.Evaluate(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(openKinds(t, svc, user), nudges.KindStreakAtRisk) {
		t.Fatal("no streak warning while a rest day is held")
	}
}
