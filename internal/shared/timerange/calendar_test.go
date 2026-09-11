package timerange_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func TestStartOfDay(t *testing.T) {
	lisbon := mustLoad(t, "Europe/Lisbon")

	cases := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{
			name: "midday",
			at:   time.Date(2026, 9, 11, 14, 50, 7, 0, lisbon),
			want: time.Date(2026, 9, 11, 0, 0, 0, 0, lisbon),
		},
		{
			name: "already midnight is unchanged",
			at:   time.Date(2026, 9, 11, 0, 0, 0, 0, lisbon),
			want: time.Date(2026, 9, 11, 0, 0, 0, 0, lisbon),
		},
		{
			name: "last second of the day stays on that day",
			at:   time.Date(2026, 9, 11, 23, 59, 59, 0, lisbon),
			want: time.Date(2026, 9, 11, 0, 0, 0, 0, lisbon),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := timerange.StartOfDay(tc.at); !got.Equal(tc.want) {
				t.Fatalf("StartOfDay(%s) = %s, want %s", tc.at, got, tc.want)
			}
		})
	}
}

// A day that has no midnight must still produce the first instant of that day,
// not the last instant of the day before. Chile skips from 23:59 to 01:00 on
// the spring transition, so time.Date normalises the requested 00:00 forward.
func TestStartOfDayWhenMidnightDoesNotExist(t *testing.T) {
	santiago := mustLoad(t, "America/Santiago")
	at := time.Date(2019, 9, 8, 15, 0, 0, 0, santiago)

	got := timerange.StartOfDay(at)

	if got.Year() != 2019 || got.Month() != time.September || got.Day() != 8 {
		t.Fatalf("StartOfDay landed on %s, want 2019-09-08", got.Format("2006-01-02 15:04 MST"))
	}
	if got.After(at) {
		t.Fatalf("StartOfDay(%s) = %s, which is after the input", at, got)
	}

	// The bug this guards: resolving the missing midnight backwards puts the
	// day boundary at 23:00 on the 7th, and every half-open [start, start+1d)
	// bucket then claims the previous evening.
	previousEvening := time.Date(2019, 9, 7, 23, 30, 0, 0, santiago)
	if !previousEvening.Before(got) {
		t.Fatalf("23:30 on the 7th is not before the 8th's start (%s); the day boundary swallowed the evening before", got)
	}
}

func TestStartOfWeekIsMonday(t *testing.T) {
	lisbon := mustLoad(t, "Europe/Lisbon")
	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, lisbon)

	for day := 0; day < 7; day++ {
		at := monday.AddDate(0, 0, day).Add(13 * time.Hour)
		if got := timerange.StartOfWeek(at); !got.Equal(monday) {
			t.Fatalf("StartOfWeek(%s, a %s) = %s, want %s", at, at.Weekday(), got, monday)
		}
	}
}

// Sunday is 0 in Go's Weekday, which is the off-by-one every hand-rolled
// version of this function gets wrong: a Sunday must belong to the week that
// began six days earlier, not the one starting tomorrow.
func TestStartOfWeekSundayBelongsToTheWeekBefore(t *testing.T) {
	lisbon := mustLoad(t, "Europe/Lisbon")
	sunday := time.Date(2026, 9, 13, 23, 30, 0, 0, lisbon)

	got := timerange.StartOfWeek(sunday)

	want := time.Date(2026, 9, 7, 0, 0, 0, 0, lisbon)
	if !got.Equal(want) {
		t.Fatalf("StartOfWeek(%s) = %s, want %s", sunday, got, want)
	}
}

// Across a DST boundary the week is not 7×24 hours long. Stepping with a
// duration rather than AddDate lands an hour off and puts Sunday in the wrong
// week; this asserts the week still spans exactly seven calendar days.
func TestStartOfWeekAcrossDST(t *testing.T) {
	london := mustLoad(t, "Europe/London")

	// 2026-03-29 is the spring-forward Sunday: that week is 167 hours long.
	sunday := time.Date(2026, 3, 29, 12, 0, 0, 0, london)
	start := timerange.StartOfWeek(sunday)

	want := time.Date(2026, 3, 23, 0, 0, 0, 0, london)
	if !start.Equal(want) {
		t.Fatalf("StartOfWeek(%s) = %s, want %s", sunday, start, want)
	}
	if end := start.AddDate(0, 0, 7); end.Sub(start) != 167*time.Hour {
		t.Fatalf("week spanned %s, want 167h across the spring transition", end.Sub(start))
	}
}

// The location comes from the time, and callers convert first. The same
// instant belongs to different weeks in different zones, which is the whole
// reason this is not done in UTC.
func TestStartOfWeekUsesTheTimesOwnLocation(t *testing.T) {
	lisbon := mustLoad(t, "Europe/Lisbon")
	auckland := mustLoad(t, "Pacific/Auckland")

	// 23:30 on Sunday in Lisbon is already Monday lunchtime in Auckland.
	instant := time.Date(2026, 9, 13, 23, 30, 0, 0, lisbon)

	inLisbon := timerange.StartOfWeek(instant.In(lisbon))
	inAuckland := timerange.StartOfWeek(instant.In(auckland))

	if inLisbon.Equal(inAuckland) {
		t.Fatalf("both zones resolved to %s; the same instant should fall in different weeks", inLisbon)
	}
	if want := time.Date(2026, 9, 7, 0, 0, 0, 0, lisbon); !inLisbon.Equal(want) {
		t.Fatalf("Lisbon week start = %s, want %s", inLisbon, want)
	}
	if want := time.Date(2026, 9, 14, 0, 0, 0, 0, auckland); !inAuckland.Equal(want) {
		t.Fatalf("Auckland week start = %s, want %s", inAuckland, want)
	}
}
