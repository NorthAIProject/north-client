package strava_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// The window is half-open, [since, until). An activity exactly on the opening
// edge is in and one exactly on the closing edge is out, so consecutive
// windows tile the calendar without a row landing in two weeks or neither.
// This is the off-by-one that makes a Sunday night session vanish.
func TestActivitiesBetweenIsHalfOpen(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "between@north.test")
	repo := strava.NewRepository(pool, nil)

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 7)

	saveActivity(t, repo, user.ID, 1, since.Add(-time.Second), 5000, 100) // just before
	saveActivity(t, repo, user.ID, 2, since, 10000, 200)                  // on the open edge
	saveActivity(t, repo, user.ID, 3, since.AddDate(0, 0, 3), 12000, 300) // inside
	saveActivity(t, repo, user.ID, 4, until, 8000, 400)                   // on the closed edge

	got, err := repo.ActivitiesBetween(context.Background(), user.ID, since, until)
	if err != nil {
		t.Fatalf("activities between: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d activities, want the two inside the half-open window", len(got))
	}
	if got[0].StravaID != 2 || got[1].StravaID != 3 {
		t.Errorf("got ids %d and %d, want 2 (open edge) and 3 (inside)", got[0].StravaID, got[1].StravaID)
	}
}

// Ascending, because the terrain builder walks weeks in the order it lays
// them out. Getting this backwards would not fail anything loudly — it would
// just draw the calendar with time running the wrong way.
func TestActivitiesBetweenIsOldestFirst(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "ordered@north.test")
	repo := strava.NewRepository(pool, nil)

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	// Saved newest-first, so a query that preserved insertion order would pass
	// a weaker version of this test.
	saveActivity(t, repo, user.ID, 3, since.AddDate(0, 0, 5), 3000, 30)
	saveActivity(t, repo, user.ID, 1, since.AddDate(0, 0, 1), 1000, 10)
	saveActivity(t, repo, user.ID, 2, since.AddDate(0, 0, 3), 2000, 20)

	got, err := repo.ActivitiesBetween(context.Background(), user.ID, since, since.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("activities between: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d activities, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].StartDate.Before(got[i-1].StartDate) {
			t.Fatalf("activity %d starts before the one before it; the window is not ascending", i)
		}
	}
}

func TestActivitiesBetweenIgnoresAnotherPersonsActivities(t *testing.T) {
	pool := testdb.New(t)
	repo := strava.NewRepository(pool, nil)
	mine := seedUser(t, pool, "mine-between@north.test")
	theirs := seedUser(t, pool, "theirs-between@north.test")

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	saveActivity(t, repo, mine.ID, 1, since.AddDate(0, 0, 1), 5000, 50)
	saveActivity(t, repo, theirs.ID, 2, since.AddDate(0, 0, 2), 9000, 90)

	got, err := repo.ActivitiesBetween(context.Background(), mine.ID, since, since.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("activities between: %v", err)
	}

	if len(got) != 1 || got[0].StravaID != 1 {
		t.Fatalf("got %d activities %+v, want only my own", len(got), got)
	}
}

// A quiet window is an empty slice, not an error. Most weeks in most accounts
// are quiet, and the terrain draws rest days rather than skipping them.
func TestActivitiesBetweenOfAQuietWindowIsEmpty(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "quiet-between@north.test")

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	got, err := strava.NewRepository(pool, nil).
		ActivitiesBetween(context.Background(), user.ID, since, since.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("activities between: %v", err)
	}

	if len(got) != 0 {
		t.Fatalf("got %d activities, want none", len(got))
	}
}

// The terminus. A nil time is "there is no more ground", which is what stops
// the scene requesting pages that will always come back empty.
func TestOldestBeforeIsNilWhenNothingIsOlder(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "terminus@north.test")
	repo := strava.NewRepository(pool, nil)

	cursor := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	saveActivity(t, repo, user.ID, 1, cursor.AddDate(0, 0, 2), 5000, 50)

	got, err := repo.OldestBefore(context.Background(), user.ID, cursor)
	if err != nil {
		t.Fatalf("oldest before: %v", err)
	}

	if got != nil {
		t.Fatalf("OldestBefore = %s, want nil: the only activity is newer than the cursor", got)
	}
}

func TestOldestBeforeFindsTheEarliestOlderActivity(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "terminus-found@north.test")
	repo := strava.NewRepository(pool, nil)

	cursor := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	earliest := cursor.AddDate(0, -6, 0)

	saveActivity(t, repo, user.ID, 1, cursor.AddDate(0, 0, -3), 5000, 50)
	saveActivity(t, repo, user.ID, 2, earliest, 9000, 90)
	saveActivity(t, repo, user.ID, 3, cursor.AddDate(0, 0, 4), 3000, 30) // newer, ignored

	got, err := repo.OldestBefore(context.Background(), user.ID, cursor)
	if err != nil {
		t.Fatalf("oldest before: %v", err)
	}

	if got == nil {
		t.Fatal("OldestBefore = nil, want the six-month-old activity")
	}
	if !got.Equal(earliest) {
		t.Errorf("OldestBefore = %s, want %s", got, earliest)
	}
}

func TestOldestBeforeIgnoresAnotherPersonsHistory(t *testing.T) {
	pool := testdb.New(t)
	repo := strava.NewRepository(pool, nil)
	mine := seedUser(t, pool, "mine-terminus@north.test")
	theirs := seedUser(t, pool, "theirs-terminus@north.test")

	cursor := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	saveActivity(t, repo, theirs.ID, 1, cursor.AddDate(-1, 0, 0), 9000, 90)

	got, err := repo.OldestBefore(context.Background(), mine.ID, cursor)
	if err != nil {
		t.Fatalf("oldest before: %v", err)
	}

	if got != nil {
		t.Fatalf("OldestBefore = %s, want nil: that history belongs to someone else", got)
	}
}

// A time carrying a *time.Location must reach Postgres as the instant it
// names, not as its wall clock reinterpreted as UTC. The whole terrain rests
// on this: the bucket boundaries are local midnights, and if they arrive
// shifted, every day is off by the zone's offset.
func TestWindowBoundariesSurviveTheirTimezone(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "tz-window@north.test")
	repo := strava.NewRepository(pool, nil)

	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	// Midnight in Auckland on 11 September 2026 is 12:00 UTC on the 10th.
	since := time.Date(2026, 9, 11, 0, 0, 0, 0, auckland)
	until := since.AddDate(0, 0, 1)

	// 11:00 UTC on the 10th is 23:00 Auckland on the 10th — the evening
	// before, and outside the window.
	saveActivity(t, repo, user.ID, 1, time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC), 5000, 50)
	// 13:00 UTC on the 10th is 01:00 Auckland on the 11th — inside.
	saveActivity(t, repo, user.ID, 2, time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC), 6000, 60)

	got, err := repo.ActivitiesBetween(context.Background(), user.ID, since, until)
	if err != nil {
		t.Fatalf("activities between: %v", err)
	}

	if len(got) != 1 || got[0].StravaID != 2 {
		t.Fatalf("got %d activities %+v, want only the one after local midnight", len(got), got)
	}
}
