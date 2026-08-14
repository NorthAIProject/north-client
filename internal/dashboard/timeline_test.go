package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
)

func TestTimelineEmptyForNewUser(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "timeline-empty@north.test")
	svc := newDashboard(t, pool)

	feed, err := svc.Timeline(ctx, user, defaultRange(user), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 0 {
		t.Fatalf("feed = %d entries, want 0", len(feed))
	}
}

// The feed's whole reason to exist: one window, several slices, one ordered
// list. If the merge drops a kind, this is what catches it.
func TestTimelineMergesEveryKindNewestFirst(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "timeline-merge@north.test")
	svc := newDashboard(t, pool)

	goalSvc := goals.NewService(goals.NewRepository(pool))
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	sleepSvc := sleep.NewService(sleep.NewRepository(pool))
	habitSvc := habits.NewService(habits.NewRepository(pool))

	if _, err := checkinSvc.UpsertToday(ctx, user, checkins.Input{
		Mood: 4, Energy: 3, Wins: "walked to the river",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hydrationSvc.Log(ctx, user, hydration.Bottle); err != nil {
		t.Fatal(err)
	}
	if _, err := sleepSvc.LogToday(ctx, user, sleep.Input{DurationMinutes: 445}); err != nil {
		t.Fatal(err)
	}

	g, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title: "Run a 10k", Category: goals.CategoryFitness,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goalSvc.AddUpdate(ctx, g.ID, user.ID, "Managed 6k today", nil); err != nil {
		t.Fatal(err)
	}

	h, err := habitSvc.Create(ctx, user, habits.Input{
		Name:   "Read before bed",
		Domain: lifedomain.Personal,
		Active: true,
		Days: []time.Weekday{
			time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
			time.Thursday, time.Friday, time.Saturday,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := habitSvc.Complete(ctx, user, h.ID); err != nil {
		t.Fatal(err)
	}

	feed, err := svc.Timeline(ctx, user, defaultRange(user), 50)
	if err != nil {
		t.Fatal(err)
	}

	seen := make(map[dashboard.EntryKind]int)
	for _, e := range feed {
		seen[e.Kind]++
	}
	for _, want := range []dashboard.EntryKind{
		dashboard.KindCheckIn,
		dashboard.KindHydration,
		dashboard.KindSleep,
		dashboard.KindHabit,
		dashboard.KindGoal,
		dashboard.KindGoalNote,
	} {
		if seen[want] == 0 {
			t.Errorf("no %s entry in the feed", want)
		}
	}

	for i := 1; i < len(feed); i++ {
		if feed[i].At.After(feed[i-1].At) {
			t.Fatalf("feed is not newest-first at %d: %v after %v",
				i, feed[i].At, feed[i-1].At)
		}
	}

	for _, e := range feed {
		if e.Title == "" {
			t.Errorf("%s entry has no title", e.Kind)
		}
		if e.Icon == "" {
			t.Errorf("%s entry has no icon", e.Kind)
		}
	}
}

func TestTimelineRespectsLimit(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "timeline-limit@north.test")
	svc := newDashboard(t, pool)
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))

	for range 8 {
		if _, err := hydrationSvc.Log(ctx, user, hydration.Glass); err != nil {
			t.Fatal(err)
		}
	}

	feed, err := svc.Timeline(ctx, user, defaultRange(user), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 3 {
		t.Fatalf("feed = %d entries, want 3", len(feed))
	}
}

// Yesterday's window must not contain today's rows. This is the check that the
// half-open [Since, Until) bounds actually reach the SQL.
func TestTimelineExcludesRowsOutsideTheWindow(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "timeline-window@north.test")
	svc := newDashboard(t, pool)
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))

	if _, err := hydrationSvc.Log(ctx, user, hydration.Litre); err != nil {
		t.Fatal(err)
	}

	today, err := svc.Timeline(ctx, user, defaultRange(user), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(today) == 0 {
		t.Fatal("expected today's entry")
	}

	yesterday, err := svc.Timeline(ctx, user,
		timerange.Parse(timerange.KeyYesterday, user.Location()), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(yesterday) != 0 {
		t.Fatalf("yesterday's window leaked %d of today's entries", len(yesterday))
	}
}

func TestTimelineIsScopedToOneUser(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "timeline-mine@north.test")
	stranger := seedUser(t, pool, "timeline-theirs@north.test")
	svc := newDashboard(t, pool)
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))

	if _, err := hydrationSvc.Log(ctx, user, hydration.Litre); err != nil {
		t.Fatal(err)
	}

	feed, err := svc.Timeline(ctx, stranger, defaultRange(stranger), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 0 {
		t.Fatalf("stranger saw %d of someone else's entries", len(feed))
	}
}
