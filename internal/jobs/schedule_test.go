package jobs

import (
	"testing"
	"time"
)

// Periodic work is aligned to the clock rather than to whenever this process
// happened to start.
func TestUntilNextAlignsToTheWallClock(t *testing.T) {
	cases := []struct {
		name     string
		now      string
		interval time.Duration
		want     time.Duration
	}{
		{"hourly from just past the hour", "2026-08-17T10:05:00Z", time.Hour, 55 * time.Minute},
		{"hourly from just before the hour", "2026-08-17T10:59:30Z", time.Hour, 30 * time.Second},
		{"hourly exactly on the hour waits a full hour", "2026-08-17T10:00:00Z", time.Hour, time.Hour},
		{"quarter-hourly", "2026-08-17T10:07:00Z", 15 * time.Minute, 8 * time.Minute},
		{"daily", "2026-08-17T10:00:00Z", 24 * time.Hour, 14 * time.Hour},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			if got := untilNext(now, tc.interval); got != tc.want {
				t.Fatalf("untilNext(%s, %s) = %s, want %s", tc.now, tc.interval, got, tc.want)
			}
		})
	}
}

// The bug this replaced: a ticker's first tick comes a full interval after the
// process starts, so a worker restarting more often than its longest interval
// never fired that job at all. Aligned, the next boundary is always within one
// interval however recently the process booted.
func TestAWorkerThatRestartsOftenStillReachesABoundary(t *testing.T) {
	start, err := time.Parse(time.RFC3339, "2026-08-17T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	// Restart every 50 minutes for a day. Under the old ticker this schedule
	// never produced a single hourly run.
	const restartEvery = 50 * time.Minute
	fired := 0
	for at := start; at.Before(start.Add(24 * time.Hour)); at = at.Add(restartEvery) {
		if untilNext(at, time.Hour) <= restartEvery {
			fired++
		}
	}

	if fired == 0 {
		t.Fatal("a worker restarting every 50 minutes never reached an hourly boundary")
	}
	// Every restart window of 50 minutes contains an hour boundary except when
	// it starts exactly on one, so this should be nearly every window.
	if fired < 20 {
		t.Fatalf("only %d of ~29 restart windows reached a boundary", fired)
	}
}

// Two workers started at different times agree on when the work runs.
func TestAlignmentIsIndependentOfWhenAProcessStarted(t *testing.T) {
	early, _ := time.Parse(time.RFC3339, "2026-08-17T10:03:00Z")
	late, _ := time.Parse(time.RFC3339, "2026-08-17T10:47:00Z")

	if early.Add(untilNext(early, time.Hour)) != late.Add(untilNext(late, time.Hour)) {
		t.Fatal("two workers booted at different times fire at different instants")
	}
}

// A non-positive interval is a programming error; waiting beats spinning.
func TestUntilNextRefusesToSpin(t *testing.T) {
	now := time.Now()
	for _, bad := range []time.Duration{0, -time.Second} {
		if got := untilNext(now, bad); got <= 0 {
			t.Fatalf("untilNext with interval %s returned %s, which would spin", bad, got)
		}
	}
}
