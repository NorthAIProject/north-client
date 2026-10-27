package checkin

import "time"

// RestsFrom is the first day a rest can bridge.
//
// Rests are derived from the check-in history, never stored. Without a start
// they would bridge gaps people lived through months ago: streaks would jump
// overnight on the dashboard and the friends leaderboard, and the marks in
// between would never be seen. Rests are still earned from older days; they
// just cannot be spent before this one.
var RestsFrom = time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)

const (
	// restEvery is how many check-in days in a run earn one rest day.
	restEvery = 7
	// restsMax is how many rest days a person can hold at once.
	restsMax = 2
)

// DayState is how one day of the week reads. On the wire it is open-ended:
// clients must tolerate values they do not know.
type DayState string

const (
	DayChecked DayState = "checked"
	// DayRest is a missed day a rest bridged.
	DayRest DayState = "rest"
	// DayNone is a past day with neither a check-in nor a rest. Deliberately
	// not "missed": a pause must never read as failure.
	DayNone     DayState = "none"
	DayToday    DayState = "today"
	DayUpcoming DayState = "upcoming"
)

// WeekDay is one day of the current Monday-to-Sunday week.
type WeekDay struct {
	Date  time.Time
	State DayState
}

// Run is a person's check-in streak with its rest days, as of one day.
type Run struct {
	// Current counts check-in days in the run that is still alive. A rest
	// bridges a missed day without adding to it: "12 days (1 rest day)".
	Current int
	// Best is the longest run in the history given.
	Best int
	// RestsHeld is how many rest days are in hand, 0 to restsMax.
	RestsHeld int
	// RestsUsed is how many rest days the current run has spent.
	RestsUsed int
	// PreviousRun is the length of the run that ended before this one, 0
	// when none has. A new run is only worth marking after one worth the
	// name: somebody checking in every other day starts a new run each time.
	PreviousRun int
	Week        [7]WeekDay
	// PerfectWeek is every day of this week so far checked in, today checked
	// or still open, and at least one done. A rest day keeps the run, not the
	// perfect week.
	PerfectWeek bool
	// WeekComplete is all seven days of this week checked in.
	WeekComplete    bool
	RestEarnedToday bool
}

// ComputeRun walks the check-in days forward from the oldest to today.
//
// Every restEvery-th check-in day of a run earns a rest, up to restsMax held.
// A missed day that has fully passed spends one when one is held and the day
// is on or after restsFrom; otherwise the run ends. Today without a check-in
// is still open, not missed, so a run ending yesterday is alive.
//
// Walking forward means what someone saw yesterday is never rewritten by
// today's computation. Days are compared as calendar dates, whatever zone
// each time carries: stored dates arrive as midnight UTC and today as
// midnight in the person's zone.
func ComputeRun(dates []time.Time, today, restsFrom time.Time) Run {
	today = calendarDay(today)
	restsFrom = calendarDay(restsFrom)

	have := make(map[time.Time]bool, len(dates))
	var first time.Time
	for _, d := range dates {
		d = calendarDay(d)
		if d.After(today) {
			continue
		}
		have[d] = true
		if first.IsZero() || d.Before(first) {
			first = d
		}
	}

	var run Run
	rested := map[time.Time]bool{}
	sinceEarn := 0
	for day := first; len(have) > 0 && !day.After(today); day = day.AddDate(0, 0, 1) {
		switch {
		case have[day]:
			run.Current++
			sinceEarn++
			if sinceEarn == restEvery {
				sinceEarn = 0
				if run.RestsHeld < restsMax {
					run.RestsHeld++
					run.RestEarnedToday = day.Equal(today)
				}
			}
			run.Best = max(run.Best, run.Current)
		case day.Equal(today):
			// Still open.
		case run.RestsHeld > 0 && !day.Before(restsFrom):
			run.RestsHeld--
			run.RestsUsed++
			rested[day] = true
		default:
			// A second missed day in a row ends nothing new.
			previous := run.PreviousRun
			if run.Current > 0 {
				previous = run.Current
			}
			run = Run{Best: run.Best, PreviousRun: previous}
			sinceEarn = 0
		}
	}

	run.Week, run.PerfectWeek, run.WeekComplete = weekOf(today, have, rested)
	return run
}

func weekOf(today time.Time, have, rested map[time.Time]bool) (days [7]WeekDay, perfect, complete bool) {
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	perfect = true
	checked := 0
	for i := range days {
		day := monday.AddDate(0, 0, i)
		state := DayNone
		switch {
		case have[day]:
			state = DayChecked
			checked++
		case day.Equal(today):
			state = DayToday
		case day.After(today):
			state = DayUpcoming
		case rested[day]:
			state = DayRest
			perfect = false
		default:
			perfect = false
		}
		days[i] = WeekDay{Date: day, State: state}
	}
	return days, perfect && checked > 0, checked == len(days)
}

// calendarDay is the date t names, as midnight UTC. It reads the fields in
// t's own zone rather than converting: converting moves a local midnight
// onto the previous day for every positive offset.
func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
