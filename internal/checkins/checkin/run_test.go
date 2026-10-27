package checkin_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
)

// The run tests use a fixed calendar: Wednesday 2 December 2026, in the week
// that starts Monday 30 November. Rest days have been bridging since well
// before it, unless a case says otherwise.
var (
	runToday  = time.Date(2026, time.December, 2, 0, 0, 0, 0, time.UTC)
	restsOpen = time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)
)

// ago returns the days that many days before today; 0 is today itself.
func ago(today time.Time, offsets ...int) []time.Time {
	out := make([]time.Time, 0, len(offsets))
	for _, n := range offsets {
		out = append(out, today.AddDate(0, 0, -n))
	}
	return out
}

// span returns every day from `from` days ago up to `to` days ago, inclusive.
func span(today time.Time, from, to int) []time.Time {
	var out []time.Time
	for n := from; n >= to; n-- {
		out = append(out, today.AddDate(0, 0, -n))
	}
	return out
}

func join(parts ...[]time.Time) []time.Time {
	var out []time.Time
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestComputeRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                      string
		dates                     []time.Time
		restsFrom                 time.Time
		current, best, held, used int
	}{
		{name: "no check-ins", dates: nil},
		{name: "today only", dates: ago(runToday, 0), current: 1, best: 1},
		// Today is still open: the run ending yesterday is alive, as it
		// always has been.
		{name: "yesterday only", dates: ago(runToday, 1), current: 1, best: 1},
		{name: "two days ago only", dates: ago(runToday, 2), best: 1},
		{name: "six days then a miss breaks", dates: join(span(runToday, 7, 2), ago(runToday, 0)), current: 1, best: 6},
		{name: "seven days earn a rest", dates: span(runToday, 6, 0), current: 7, best: 7, held: 1},
		// The rest bridges the missed day without counting it.
		{name: "seven, a miss, then today", dates: join(span(runToday, 8, 2), ago(runToday, 0)), current: 8, best: 8, used: 1},
		{name: "a rest covers yesterday while today is open", dates: span(runToday, 8, 2), current: 7, best: 7, used: 1},
		{name: "two misses with one rest break", dates: join(span(runToday, 9, 3), ago(runToday, 0)), current: 1, best: 7},
		{name: "fourteen, two misses, today", dates: join(span(runToday, 16, 3), ago(runToday, 0)), current: 15, best: 15, used: 2},
		{name: "rests held cap at two", dates: span(runToday, 20, 0), current: 21, best: 21, held: 2},
		// Rests can be earned from old check-ins, but only bridge a gap from
		// the day the feature shipped, so nobody's history is rewritten.
		{name: "a miss before rests began breaks", dates: join(span(runToday, 8, 2), ago(runToday, 0)), restsFrom: runToday, current: 1, best: 7},
		{
			name:    "best survives broken runs",
			dates:   join(span(runToday, 30, 21), span(runToday, 12, 8), ago(runToday, 0)),
			current: 1, best: 10,
		},
		{name: "a future date is ignored", dates: ago(runToday, -1, 0), current: 1, best: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			from := tc.restsFrom
			if from.IsZero() {
				from = restsOpen
			}
			got := checkin.ComputeRun(tc.dates, runToday, from)
			if got.Current != tc.current || got.Best != tc.best || got.RestsHeld != tc.held || got.RestsUsed != tc.used {
				t.Fatalf("current/best/held/used = %d/%d/%d/%d, want %d/%d/%d/%d",
					got.Current, got.Best, got.RestsHeld, got.RestsUsed, tc.current, tc.best, tc.held, tc.used)
			}
		})
	}
}

// A new run is worth marking only after a run worth the name. Somebody who
// checks in every other day starts a "new run" every time, and telling them
// so each time is wallpaper pointing at what they lost.
func TestComputeRunRemembersTheRunThatEnded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		dates    []time.Time
		previous int
	}{
		{"after a five-day run and a long gap", join(span(runToday, 30, 21), span(runToday, 12, 8), ago(runToday, 0)), 5},
		{"every other day", ago(runToday, 4, 2, 0), 1},
		{"no run before", ago(runToday, 0), 0},
		{"the run is still going", span(runToday, 3, 0), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := checkin.ComputeRun(tc.dates, runToday, restsOpen).PreviousRun; got != tc.previous {
				t.Fatalf("previous run = %d, want %d", got, tc.previous)
			}
		})
	}
}

func TestComputeRunMarksTheRestEarnedToday(t *testing.T) {
	t.Parallel()
	if got := checkin.ComputeRun(span(runToday, 6, 0), runToday, restsOpen); !got.RestEarnedToday {
		t.Fatal("the seventh day in a row, today, should earn a rest today")
	}
	if got := checkin.ComputeRun(span(runToday, 7, 1), runToday, restsOpen); got.RestEarnedToday {
		t.Fatal("a rest earned yesterday is not earned today")
	}
}

// Stored dates come back as midnight UTC; today is midnight in the person's
// zone. Comparing instants instead of calendar days moved every check-in onto
// the previous day for positive offsets — the bug service.go's dateKey warns
// about. The run must agree east and west of UTC.
func TestComputeRunComparesCalendarDaysNotInstants(t *testing.T) {
	t.Parallel()
	for _, zone := range []string{"Pacific/Auckland", "America/Los_Angeles", "Europe/Lisbon"} {
		t.Run(zone, func(t *testing.T) {
			t.Parallel()
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Skipf("no tzdata for %s: %v", zone, err)
			}
			today := time.Date(2026, time.December, 2, 0, 0, 0, 0, loc)
			stored := span(runToday, 2, 0) // midnight UTC, as Postgres returns them
			if got := checkin.ComputeRun(stored, today, restsOpen); got.Current != 3 {
				t.Fatalf("current = %d, want 3", got.Current)
			}
		})
	}
}

// The two weeks the clocks change in 2026 (EU 25 October, US 1 November) must
// count like any other.
func TestComputeRunAcrossClockChanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		zone  string
		today time.Time
	}{
		{"Europe/Berlin", time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)},
		{"America/New_York", time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			t.Parallel()
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Skipf("no tzdata for %s: %v", tc.zone, err)
			}
			today := time.Date(tc.today.Year(), tc.today.Month(), tc.today.Day(), 0, 0, 0, 0, loc)
			if got := checkin.ComputeRun(span(tc.today, 6, 0), today, restsOpen); got.Current != 7 {
				t.Fatalf("current = %d, want 7", got.Current)
			}
		})
	}
}

func states(r checkin.Run) []checkin.DayState {
	out := make([]checkin.DayState, len(r.Week))
	for i, d := range r.Week {
		out[i] = d.State
	}
	return out
}

func sameStates(a, b []checkin.DayState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestComputeRunWeek(t *testing.T) {
	t.Parallel()
	const (
		c = checkin.DayChecked
		r = checkin.DayRest
		n = checkin.DayNone
		o = checkin.DayToday
		u = checkin.DayUpcoming
	)
	monday := time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC)
	sunday := time.Date(2026, time.December, 6, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name              string
		today             time.Time
		dates             []time.Time
		want              []checkin.DayState
		perfect, complete bool
	}{
		{
			name: "perfect so far with today open", today: runToday,
			dates: ago(runToday, 2, 1),
			want:  []checkin.DayState{c, c, o, u, u, u, u}, perfect: true,
		},
		{
			name: "perfect so far with today checked", today: runToday,
			dates: ago(runToday, 2, 1, 0),
			want:  []checkin.DayState{c, c, c, u, u, u, u}, perfect: true,
		},
		{
			// A rest keeps the run, not the perfect week.
			name: "a rest day in the week", today: runToday,
			dates: join(span(runToday, 9, 3), ago(runToday, 1, 0)),
			want:  []checkin.DayState{r, c, c, u, u, u, u},
		},
		{
			name: "a plain gap reads as none, never missed", today: runToday,
			dates: ago(runToday, 1, 0),
			want:  []checkin.DayState{n, c, c, u, u, u, u},
		},
		{
			// Nothing done yet this week is not a perfect week.
			name: "Monday before checking in", today: monday,
			dates: span(monday, 7, 1),
			want:  []checkin.DayState{o, u, u, u, u, u, u},
		},
		{
			name: "seven of seven on Sunday", today: sunday,
			dates: span(sunday, 6, 0),
			want:  []checkin.DayState{c, c, c, c, c, c, c}, perfect: true, complete: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkin.ComputeRun(tc.dates, tc.today, restsOpen)
			if !sameStates(states(got), tc.want) {
				t.Fatalf("week = %v, want %v", states(got), tc.want)
			}
			if got.PerfectWeek != tc.perfect || got.WeekComplete != tc.complete {
				t.Fatalf("perfect/complete = %v/%v, want %v/%v", got.PerfectWeek, got.WeekComplete, tc.perfect, tc.complete)
			}
		})
	}

	got := checkin.ComputeRun(nil, runToday, restsOpen)
	if !got.Week[0].Date.Equal(monday) || !got.Week[6].Date.Equal(sunday) {
		t.Fatalf("week runs %s..%s, want %s..%s", got.Week[0].Date, got.Week[6].Date, monday, sunday)
	}
}
