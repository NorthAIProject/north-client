package fitness

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
)

func week(t *testing.T, start time.Time, sessions int) strava.TerrainWeek {
	t.Helper()
	w := strava.TerrainWeek{Start: start}
	for i := range w.Days {
		w.Days[i] = strava.TerrainDay{Date: start.AddDate(0, 0, i), Weekday: start.AddDate(0, 0, i).Weekday()}
	}
	if sessions > 0 {
		w.Days[0].Sessions = sessions
		w.Days[0].LoadMETMin = 300
		w.LoadMETMin = 300
	}
	return w
}

// The bug this page shipped with: the handler set Unavailable, a comment
// promised the template would say the activities could not be read, and the
// template never tested for it. A failed read rendered as "nothing imported
// yet" — telling someone their history was empty when it was merely
// unreadable.
func TestUnavailableIsNotMistakenForEmpty(t *testing.T) {
	t.Parallel()

	got := terrainState(strava.Status{Configured: true, Connected: true, Unavailable: true}, strava.TerrainPage{})

	if got == stateEmpty {
		t.Fatal("an unreadable history rendered as an empty one")
	}
	if got != stateUnavailable {
		t.Errorf("state = %v, want stateUnavailable", got)
	}
}

func TestTerrainStates(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	busy := strava.TerrainPage{Weeks: []strava.TerrainWeek{week(t, monday, 2)}}
	quiet := strava.TerrainPage{Weeks: []strava.TerrainWeek{week(t, monday, 0)}}

	tests := []struct {
		name   string
		status strava.Status
		page   strava.TerrainPage
		want   terrainStateKind
	}{
		{"no credentials on the server", strava.Status{}, busy, stateUnconfigured},
		{"configured but not connected", strava.Status{Configured: true}, busy, stateDisconnected},
		{"connected with activities", strava.Status{Configured: true, Connected: true}, busy, stateTerrain},
		{"connected with nothing imported", strava.Status{Configured: true, Connected: true}, quiet, stateEmpty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := terrainState(tt.status, tt.page); got != tt.want {
				t.Errorf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

// An account whose only training is older than the window is not empty. The
// window is eight weeks; someone who stopped three months ago still has a
// history, and the strip is the way back to it.
func TestQuietWindowWithOlderHistoryIsNotEmpty(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	page := strava.TerrainPage{
		Weeks:    []strava.TerrainWeek{week(t, monday, 0)},
		HasOlder: true,
	}

	if got := terrainState(strava.Status{Configured: true, Connected: true}, page); got != stateTerrain {
		t.Errorf("state = %v, want stateTerrain: there is history, just not in this window", got)
	}
}

// The tail is the only keyboard route into the past — the camera is driven by
// dragging, which has no keyboard equivalent. If this link stops rendering,
// older weeks become unreachable without a pointer.
func TestStripTailOffersTheWayBackWhenThereIsMore(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	page := strava.TerrainPage{Weeks: []strava.TerrainWeek{week(t, monday, 1)}, HasOlder: true}

	var buf strings.Builder
	if err := stripTail(page).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "Load earlier weeks") {
		t.Error("no way to reach older weeks without a pointer")
	}
	if !strings.Contains(html, "before=2026-09-07") {
		t.Errorf("tail does not carry the cursor for the page before it:\n%s", html)
	}
}

func TestStripTailSaysWhereTheRecordEnds(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	oldest := time.Date(2025, 6, 14, 0, 0, 0, 0, time.UTC)
	page := strava.TerrainPage{Weeks: []strava.TerrainWeek{week(t, monday, 1)}, OldestAt: &oldest}

	var buf strings.Builder
	if err := stripTail(page).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	if html := buf.String(); strings.Contains(html, "Load earlier weeks") {
		t.Error("the tail offers more weeks when there are none")
	}
}

// The rows are the accessible equivalent of the scene, so they have to carry
// the per-day figure the scene draws. A flat list of sessions could not be
// used to rebuild the landscape.
func TestStripCarriesThePerDayLoad(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	page := strava.TerrainPage{Weeks: []strava.TerrainWeek{week(t, monday, 1)}}

	var buf strings.Builder
	if err := ActivityStrip(page, time.UTC).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "MET-min") {
		t.Error("a day row does not state its load")
	}
	if !strings.Contains(html, "Mon 7 Sep") {
		t.Errorf("a day row does not name its date:\n%s", html)
	}
}

// Rest days are drawn in the scene but must not become empty rows in the
// strip: a reader would be scrolling past five blanks a week.
func TestStripOmitsRestDays(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	page := strava.TerrainPage{Weeks: []strava.TerrainWeek{week(t, monday, 1)}}

	var buf strings.Builder
	if err := ActivityStrip(page, time.UTC).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	if n := strings.Count(buf.String(), "aria-pressed"); n != 1 {
		t.Errorf("strip rendered %d day rows, want only the one with a session", n)
	}
}
