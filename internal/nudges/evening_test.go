package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// eveningPrefs turns the reflection on at 21:00 and the accountability nudges
// off, so each test sees only the nudge it is about.
func eveningPrefs(t *testing.T, prefs *notifications.Service, user users.User, reflection bool) {
	t.Helper()
	on, hour := reflection, 21
	if _, err := prefs.Upsert(context.Background(), user.ID, notifications.Input{
		NudgeMissedCheckIn: false,
		CoachActivity:      true,
		QuietStart:         "22:00",
		QuietEnd:           "07:00",
		EveningReflection:  &on,
		EveningHour:        &hour,
	}); err != nil {
		t.Fatal(err)
	}
}

func eveningUser(t *testing.T, email string, now time.Time) (users.User, *nudges.Service, *notifications.Service, func(time.Time) *nudges.Service) {
	t.Helper()
	pool := testdb.New(t)
	user := mustOnboard(t, pool, seedUser(t, pool, email), now.AddDate(0, 0, -30))
	prefs := notifications.NewService(notifications.NewRepository(pool))
	at := func(when time.Time) *nudges.Service { return evalService(pool, when).WithPrefs(prefs) }
	return user, at(now), prefs, func(when time.Time) *nudges.Service {
		writeCheckIn(t, pool, user.ID, time.Date(when.Year(), when.Month(), when.Day(), 0, 0, 0, 0, time.UTC))
		return at(when)
	}
}

// No check-in yet today: the reflection asks for one, with the mood buttons.
func TestEveningReflectionAsksForTheCheckIn(t *testing.T) {
	ctx := context.Background()
	evening := time.Date(2026, 9, 2, 21, 15, 0, 0, time.UTC)
	user, svc, prefs, _ := eveningUser(t, "evening-checkin@north.test", evening)
	eveningPrefs(t, prefs, user, true)

	if n, err := svc.Evaluate(ctx, user); err != nil || n != 1 {
		t.Fatalf("created = %d, err = %v; want 1", n, err)
	}
	list, err := svc.ListOpen(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != nudges.KindEveningReflection || list[0].Href != "/app/check-ins" {
		t.Fatalf("open = %#v", list)
	}
	if nudges.PushCategory(list[0].Kind) != nudges.CategoryCheckIn {
		t.Fatalf("category = %q, want the check-in buttons", nudges.PushCategory(list[0].Kind))
	}

	// One a night.
	if n, _ := svc.Evaluate(ctx, user); n != 0 {
		t.Fatalf("second sweep created %d, want 0", n)
	}
}

// Already checked in: the reflection points at the journal instead.
func TestEveningReflectionAfterACheckInOpensTheJournal(t *testing.T) {
	ctx := context.Background()
	evening := time.Date(2026, 9, 2, 21, 15, 0, 0, time.UTC)
	user, _, prefs, checkedIn := eveningUser(t, "evening-journal@north.test", evening)
	eveningPrefs(t, prefs, user, true)
	svc := checkedIn(evening)

	if n, err := svc.Evaluate(ctx, user); err != nil || n != 1 {
		t.Fatalf("created = %d, err = %v; want 1", n, err)
	}
	list, _ := svc.ListOpen(ctx, user.ID, 10)
	if len(list) != 1 || list[0].Href != "/app/mind" {
		t.Fatalf("open = %#v", list)
	}
}

// Off unless asked for, before the hour, and after the window.
func TestEveningReflectionKeepsToItsHour(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		at   time.Time
		on   bool
	}{
		{"off by default", time.Date(2026, 9, 2, 21, 15, 0, 0, time.UTC), false},
		{"before the hour", time.Date(2026, 9, 2, 20, 45, 0, 0, time.UTC), true},
		{"window closed", time.Date(2026, 9, 3, 0, 30, 0, 0, time.UTC), true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, svc, prefs, _ := eveningUser(t, "evening-hour-"+string(rune('a'+i))+"@north.test", tc.at)
			eveningPrefs(t, prefs, user, tc.on)
			// Quiet hours are off, so only the reflection's own rules decide.
			if n, err := svc.Evaluate(ctx, user); err != nil || n != 0 {
				t.Fatalf("created = %d, err = %v; want 0 (open: %v)", n, err, openKinds(t, svc, user))
			}
		})
	}
}

// A streak about to break already asks for the same check-in, more urgently.
func TestEveningReflectionGivesWayToTheStreakWarning(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	evening := time.Date(2026, 9, 2, 21, 15, 0, 0, time.UTC)
	user := streakUser(t, pool, "evening-streak@north.test", evening)
	prefs := notifications.NewService(notifications.NewRepository(pool))
	on, hour := true, 21
	if _, err := prefs.Upsert(ctx, user.ID, notifications.Input{
		NudgeMissedCheckIn: true, CoachActivity: true, QuietStart: "22:00", QuietEnd: "07:00",
		EveningReflection: &on, EveningHour: &hour,
	}); err != nil {
		t.Fatal(err)
	}

	svc := evalService(pool, evening).WithPrefs(prefs)
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	kinds := openKinds(t, svc, user)
	if len(kinds) != 1 || kinds[0] != nudges.KindStreakAtRisk {
		t.Fatalf("open = %v, want only the streak warning", kinds)
	}
}
