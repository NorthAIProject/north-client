package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeWeekly struct{ reviewed bool }

func (f fakeWeekly) Reviewed(context.Context, users.User, time.Time) (bool, error) {
	return f.reviewed, nil
}

func weekReviewPrefs(t *testing.T, prefs *notifications.Service, user users.User, weekly bool) {
	t.Helper()
	on, hour := true, 21
	if _, err := prefs.Upsert(context.Background(), user.ID, notifications.Input{
		CoachActivity:     true,
		WeeklyReportAuto:  weekly,
		QuietStart:        "23:30",
		QuietEnd:          "07:00",
		EveningReflection: &on,
		EveningHour:       &hour,
	}); err != nil {
		t.Fatal(err)
	}
}

// Sunday at the evening hour: the weekly review, in place of the evening
// reflection, once.
func TestWeekReviewOnSundayEvening(t *testing.T) {
	ctx := context.Background()
	sunday := time.Date(2026, 10, 4, 21, 10, 0, 0, time.UTC)
	user, svc, prefs, _ := eveningUser(t, "week-review@north.test", sunday)
	weekReviewPrefs(t, prefs, user, true)
	svc = svc.WithWeekly(fakeWeekly{})

	if n, err := svc.Evaluate(ctx, user); err != nil || n != 1 {
		t.Fatalf("created = %d, err = %v; want 1 (open: %v)", n, err, openKinds(t, svc, user))
	}
	list, _ := svc.ListOpen(ctx, user.ID, 10)
	if len(list) != 1 || list[0].Kind != nudges.KindWeekReview || list[0].Href != "/app/weekly" {
		t.Fatalf("open = %#v", list)
	}
	if n, _ := svc.Evaluate(ctx, user); n != 0 {
		t.Fatalf("second sweep created %d, want 0", n)
	}
}

// Not on other days, not once next week has a focus, and not with weekly
// reports switched off.
func TestWeekReviewStaysQuiet(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		at       time.Time
		reviewed bool
		weekly   bool
	}{
		{"saturday", time.Date(2026, 10, 3, 21, 10, 0, 0, time.UTC), false, true},
		{"before the hour", time.Date(2026, 10, 4, 20, 30, 0, 0, time.UTC), false, true},
		{"already planned", time.Date(2026, 10, 4, 21, 10, 0, 0, time.UTC), true, true},
		{"switched off", time.Date(2026, 10, 4, 21, 10, 0, 0, time.UTC), false, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, svc, prefs, _ := eveningUser(t, "week-quiet-"+string(rune('a'+i))+"@north.test", tc.at)
			weekReviewPrefs(t, prefs, user, tc.weekly)
			svc = svc.WithWeekly(fakeWeekly{reviewed: tc.reviewed})
			if _, err := svc.Evaluate(ctx, user); err != nil {
				t.Fatal(err)
			}
			for _, k := range openKinds(t, svc, user) {
				if k == nudges.KindWeekReview {
					t.Fatalf("week review raised")
				}
			}
		})
	}
}
