package strava

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// buildTerrain is the whole reason the bucketing is a pure function: every
// interesting case is a calendar case, and none of them need a database.

func loc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return l
}

func act(id int64, sport string, start time.Time, movingS int) Activity {
	return Activity{
		StravaID:    id,
		Name:        sport,
		SportType:   sport,
		StartDate:   start,
		MovingTimeS: movingS,
	}
}

// A window with nothing in it is still a landscape. Returning no weeks would
// leave the scene with nothing to draw and no way to say "you rested".
func TestBuildTerrainFillsEveryDayOfAQuietWindow(t *testing.T) {
	t.Parallel()
	lisbon := loc(t, "Europe/Lisbon")

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, lisbon)
	to := from.AddDate(0, 0, 21)

	weeks := buildTerrain(nil, lisbon, from, to)

	if len(weeks) != 3 {
		t.Fatalf("got %d weeks, want 3", len(weeks))
	}
	for _, week := range weeks {
		if week.Start.Weekday() != time.Monday {
			t.Errorf("week starts on %s, want Monday", week.Start.Weekday())
		}
		for i, day := range week.Days {
			if !day.Rest() {
				t.Errorf("day %d of %s is not a rest day", i, week.Start.Format(time.DateOnly))
			}
			if day.Date.IsZero() {
				t.Errorf("day %d of %s has no date", i, week.Start.Format(time.DateOnly))
			}
		}
	}
}

// The whole argument for bucketing in Go with the reader's location. The same
// instant is Sunday night in Lisbon and Monday lunchtime in Auckland, so it
// belongs to a different week depending on whose calendar is asking.
func TestBuildTerrainBucketsInTheReadersZone(t *testing.T) {
	t.Parallel()
	lisbon := loc(t, "Europe/Lisbon")
	auckland := loc(t, "Pacific/Auckland")

	instant := time.Date(2026, 9, 13, 23, 30, 0, 0, lisbon) // Sunday night

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 14)

	inLisbon := buildTerrain([]Activity{act(1, "Run", instant, 1800)}, lisbon, from, to)
	if got := inLisbon[0].Days[6].Sessions; got != 1 {
		t.Errorf("Lisbon: Sunday of the first week has %d sessions, want 1", got)
	}

	inAuckland := buildTerrain([]Activity{act(1, "Run", instant, 1800)}, auckland, from, to)
	if got := inAuckland[1].Days[0].Sessions; got != 1 {
		t.Errorf("Auckland: Monday of the second week has %d sessions, want 1", got)
	}
	if got := inAuckland[0].Days[6].Sessions; got != 0 {
		t.Errorf("Auckland: Sunday of the first week has %d sessions, want 0", got)
	}
}

// Spring forward: that week is 167 hours long. Stepping the calendar with a
// duration rather than by date would land an hour short and shift every
// following day.
func TestBuildTerrainAcrossSpringForward(t *testing.T) {
	t.Parallel()
	london := loc(t, "Europe/London")

	from := time.Date(2026, 3, 23, 0, 0, 0, 0, london)
	to := from.AddDate(0, 0, 14)

	weeks := buildTerrain(nil, london, from, to)

	if len(weeks) != 2 {
		t.Fatalf("got %d weeks, want 2", len(weeks))
	}
	for _, week := range weeks {
		for i, day := range week.Days {
			if want := time.Weekday((int(time.Monday) + i) % 7); day.Weekday != want {
				t.Errorf("week %s day %d is a %s, want %s",
					week.Start.Format(time.DateOnly), i, day.Weekday, want)
			}
		}
	}
	if got := weeks[1].Start; got.Day() != 30 || got.Month() != time.March {
		t.Errorf("second week starts %s, want 30 March", got.Format(time.DateOnly))
	}
}

// Fall back: that day is 25 hours long, so two activities exactly 24 hours
// apart in absolute time are on the same local date, not consecutive ones.
func TestBuildTerrainAcrossFallBack(t *testing.T) {
	t.Parallel()
	newYork := loc(t, "America/New_York")

	// 2026-11-01 is the fall-back Sunday in the US.
	first := time.Date(2026, 11, 1, 0, 30, 0, 0, newYork)
	second := first.Add(24 * time.Hour)

	from := time.Date(2026, 10, 26, 0, 0, 0, 0, newYork)
	to := from.AddDate(0, 0, 14)

	weeks := buildTerrain([]Activity{
		act(1, "Run", first, 1800),
		act(2, "Run", second, 1800),
	}, newYork, from, to)

	sunday := weeks[0].Days[6]
	if sunday.Weekday != time.Sunday {
		t.Fatalf("day 6 is a %s, want Sunday", sunday.Weekday)
	}
	if sunday.Sessions != 2 {
		t.Errorf("the 25-hour Sunday holds %d sessions, want 2: 24 hours apart is still the same day here", sunday.Sessions)
	}
}

// Chile skips midnight on this date. The day must still exist exactly once,
// and must not swallow the evening before it.
func TestBuildTerrainWhenADayHasNoMidnight(t *testing.T) {
	t.Parallel()
	santiago := loc(t, "America/Santiago")

	from := time.Date(2019, 9, 2, 0, 0, 0, 0, santiago)
	to := from.AddDate(0, 0, 14)

	weeks := buildTerrain([]Activity{
		// 23:30 on the 7th: the evening before the day with no midnight.
		act(1, "Run", time.Date(2019, 9, 7, 23, 30, 0, 0, santiago), 1800),
		// 09:00 on the 8th: the day itself.
		act(2, "Run", time.Date(2019, 9, 8, 9, 0, 0, 0, santiago), 1800),
	}, santiago, from, to)

	seen := map[string]int{}
	for _, week := range weeks {
		for _, day := range week.Days {
			seen[day.Date.Format(time.DateOnly)]++
		}
	}
	for date, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times, want once", date, n)
		}
	}

	saturday := weeks[0].Days[5]
	sunday := weeks[0].Days[6]
	if saturday.Sessions != 1 {
		t.Errorf("Saturday the 7th has %d sessions, want 1", saturday.Sessions)
	}
	if sunday.Sessions != 1 {
		t.Errorf("Sunday the 8th has %d sessions, want 1; the evening before must not have been swallowed", sunday.Sessions)
	}
}

// The window is half-open, matching the query that produced the rows.
func TestBuildTerrainWindowIsHalfOpen(t *testing.T) {
	t.Parallel()
	utc := time.UTC

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)
	to := from.AddDate(0, 0, 7)

	weeks := buildTerrain([]Activity{
		act(1, "Run", from, 1800),                 // on the open edge: in
		act(2, "Run", to, 1800),                   // on the closed edge: out
		act(3, "Run", from.Add(-time.Second), 60), // before: out
	}, utc, from, to)

	total := 0
	for _, week := range weeks {
		for _, day := range week.Days {
			total += day.Sessions
		}
	}
	if total != 1 {
		t.Errorf("window holds %d sessions, want only the one on the open edge", total)
	}
}

func TestBuildTerrainSumsADayAndOrdersItsSessions(t *testing.T) {
	t.Parallel()
	utc := time.UTC

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)
	morning := from.Add(7 * time.Hour)
	evening := from.Add(19 * time.Hour)

	weeks := buildTerrain([]Activity{
		act(2, "WeightTraining", evening, 3060),
		act(1, "Run", morning, 1680),
	}, utc, from, from.AddDate(0, 0, 7))

	monday := weeks[0].Days[0]
	if monday.Sessions != 2 {
		t.Fatalf("Monday has %d sessions, want 2", monday.Sessions)
	}
	if monday.MovingTimeS != 1680+3060 {
		t.Errorf("MovingTimeS = %d, want %d", monday.MovingTimeS, 1680+3060)
	}
	if monday.LoadMETMin <= 0 {
		t.Error("a day with two real sessions has no load")
	}
	if monday.Routes[0].StravaID != 1 {
		t.Errorf("first route is %d, want the morning session", monday.Routes[0].StravaID)
	}
}

// A gym session must have height. This is the whole reason the axis is
// MET-minutes and not elevation: with elevation, every indoor session on the
// page sat flat on the ground and the terrain said nothing about them.
func TestIndoorSessionsCarryLoad(t *testing.T) {
	t.Parallel()
	utc := time.UTC

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)
	weeks := buildTerrain([]Activity{
		act(1, "WeightTraining", from.Add(12*time.Hour), 3060),
	}, utc, from, from.AddDate(0, 0, 7))

	monday := weeks[0].Days[0]
	if monday.LoadMETMin <= 0 {
		t.Fatal("a 51-minute weights session has no load")
	}
	if monday.ElevationM != 0 {
		t.Fatal("test is not exercising the case it claims to")
	}
	if monday.Dominant != FamilyStrength {
		t.Errorf("Dominant = %q, want strength", monday.Dominant)
	}
}

// A sport the table has never heard of still imports, and must still have
// height. A mapping gap that silently became a rest day would erase a session
// somebody actually did.
func TestAnUnknownSportStillHasLoad(t *testing.T) {
	t.Parallel()
	utc := time.UTC

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)
	weeks := buildTerrain([]Activity{
		act(1, "Quidditch", from.Add(10*time.Hour), 3600),
	}, utc, from, from.AddDate(0, 0, 7))

	monday := weeks[0].Days[0]
	if monday.LoadMETMin <= 0 {
		t.Error("an unmapped sport produced no load; a mapping gap must not become a rest day")
	}
	if monday.Dominant != FamilyOther {
		t.Errorf("Dominant = %q, want other", monday.Dominant)
	}
}

func TestDominantFamilyAndMixedDays(t *testing.T) {
	t.Parallel()
	utc := time.UTC
	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)

	t.Run("a clear majority is not mixed", func(t *testing.T) {
		t.Parallel()
		weeks := buildTerrain([]Activity{
			act(1, "Run", from.Add(7*time.Hour), 3600),
			act(2, "WeightTraining", from.Add(19*time.Hour), 600),
		}, utc, from, from.AddDate(0, 0, 7))

		monday := weeks[0].Days[0]
		if monday.Dominant != FamilyRun {
			t.Errorf("Dominant = %q, want run", monday.Dominant)
		}
		if monday.Mixed {
			t.Error("a day dominated by one sport is marked mixed")
		}
	})

	t.Run("an even split is mixed", func(t *testing.T) {
		t.Parallel()
		// A run and a ride of comparable load: neither clears 60%.
		weeks := buildTerrain([]Activity{
			act(1, "Run", from.Add(7*time.Hour), 1800),
			act(2, "Ride", from.Add(19*time.Hour), 3000),
		}, utc, from, from.AddDate(0, 0, 7))

		monday := weeks[0].Days[0]
		if !monday.Mixed {
			t.Errorf("a day split %v is not marked mixed", monday.Mix)
		}
		if len(monday.Mix) != 2 {
			t.Errorf("Mix has %d entries, want 2", len(monday.Mix))
		}
		if monday.Mix[0].LoadMETMin < monday.Mix[1].LoadMETMin {
			t.Error("Mix is not ordered by load, descending")
		}
	})
}

// Map iteration order in Go is deliberately random. Without a total order on
// the tie-break the same day would colour itself differently between two page
// loads, which is the kind of bug that gets reported as "it flickers".
func TestDominantIsStableAcrossRuns(t *testing.T) {
	t.Parallel()
	utc := time.UTC
	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)

	// Two families with identical load and identical moving time: only the
	// declared order in Families can break this.
	activities := []Activity{
		act(1, "Ride", from.Add(7*time.Hour), 1800),
		act(2, "VirtualRide", from.Add(9*time.Hour), 1800),
		act(3, "Swim", from.Add(11*time.Hour), 1800),
	}

	first := buildTerrain(activities, utc, from, from.AddDate(0, 0, 7))[0].Days[0]
	for range 50 {
		got := buildTerrain(activities, utc, from, from.AddDate(0, 0, 7))[0].Days[0]
		if got.Dominant != first.Dominant {
			t.Fatalf("Dominant flipped between runs: %q then %q", first.Dominant, got.Dominant)
		}
		for i := range got.Mix {
			if got.Mix[i].Family != first.Mix[i].Family {
				t.Fatalf("Mix order flipped between runs at %d: %q then %q", i, first.Mix[i].Family, got.Mix[i].Family)
			}
		}
	}
}

// One enormous day must not flatten the rest. This is the failure the
// elevation-based version already had, restated as a property.
func TestLoadScaleIsNotDraggedUpByOneOutlier(t *testing.T) {
	t.Parallel()
	utc := time.UTC
	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)

	var activities []Activity
	// Twenty ordinary hour-long runs, one per day.
	for i := range 20 {
		activities = append(activities, act(int64(i+1), "Run", from.AddDate(0, 0, i).Add(7*time.Hour), 3600))
	}
	// One six-hour hike.
	activities = append(activities, act(99, "Hike", from.AddDate(0, 0, 21).Add(7*time.Hour), 6*3600))

	weeks := buildTerrain(activities, utc, from, from.AddDate(0, 0, 28))
	scale := loadScale(weeks)

	var ordinary float64
	for _, week := range weeks {
		for _, day := range week.Days {
			if day.LoadMETMin > 0 && day.LoadMETMin < 1000 {
				ordinary = day.LoadMETMin
				break
			}
		}
	}

	if ordinary/scale < 0.5 {
		t.Errorf("an ordinary day reaches only %.0f%% of the scale; the outlier has flattened the terrain",
			100*ordinary/scale)
	}
}

func TestLoadScaleHasAFloorForAThinHistory(t *testing.T) {
	t.Parallel()
	utc := time.UTC
	from := time.Date(2026, 9, 7, 0, 0, 0, 0, utc)

	weeks := buildTerrain([]Activity{
		act(1, "Walk", from.Add(9*time.Hour), 600),
	}, utc, from, from.AddDate(0, 0, 7))

	if got := loadScale(weeks); got != minLoadScale {
		t.Errorf("loadScale = %v, want the %v floor: a single short walk must not become the ceiling", got, minLoadScale)
	}
}

func TestLoadScaleOfAnEmptyTerrainIsTheFloor(t *testing.T) {
	t.Parallel()

	if got := loadScale(nil); got != minLoadScale {
		t.Errorf("loadScale(nil) = %v, want %v", got, minLoadScale)
	}
}

// Pages must tile, not overlap.
//
// A later page is asked for by naming the oldest week already drawn, so it has
// to stop where that week starts. Ending it a week later hands back a week the
// caller already has — which the scene would build twice at the same position,
// two meshes deep, with every week behind it shifted by one.
func TestTerrainPagesDoNotOverlap(t *testing.T) {
	t.Parallel()
	utc := time.UTC

	// Page 0 runs to the end of the week being lived in.
	firstEnd := startOfNextWeek(time.Date(2026, 9, 11, 10, 0, 0, 0, utc))
	y, m, d := firstEnd.Date()
	firstStart := timerange.StartOfDay(time.Date(y, m, d-daysPerWeek*8, 12, 0, 0, 0, utc))
	first := buildTerrain(nil, utc, firstStart, firstEnd)

	// The cursor the client is handed: the start of the oldest week drawn.
	cursor := first[0].Start

	// Page 1 ends there.
	y, m, d = cursor.Date()
	secondStart := timerange.StartOfDay(time.Date(y, m, d-daysPerWeek*8, 12, 0, 0, 0, utc))
	second := buildTerrain(nil, utc, secondStart, cursor)

	if len(second) == 0 {
		t.Fatal("the older page is empty")
	}

	newestOfSecond := second[len(second)-1].Start
	if !newestOfSecond.Before(cursor) {
		t.Errorf("the older page's newest week starts %s, at or after the cursor %s: the pages overlap",
			newestOfSecond.Format(time.DateOnly), cursor.Format(time.DateOnly))
	}

	seen := map[string]bool{}
	for _, w := range append(append([]TerrainWeek{}, second...), first...) {
		key := w.Start.Format(time.DateOnly)
		if seen[key] {
			t.Errorf("week %s appears in both pages", key)
		}
		seen[key] = true
	}
}
