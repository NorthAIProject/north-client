package timerange

import "time"

// Calendar boundaries, in the location the time carries.
//
// These live here rather than in each package that needs them because there
// were three of them by the time anyone looked: one in the coach sidebar, one
// inside the weekly report's Week type, and one private to this file. Three
// implementations of "when did this week start" is three chances to disagree
// about somebody's Sunday night.
//
// Neither function takes a *time.Location. The caller converts first —
// `StartOfWeek(t.In(loc))` — because a time already carries a location and
// accepting a second one invites the two to disagree silently.

// gapProbeHours bounds the search for the first real instant of a day whose
// midnight does not exist. No zone has ever skipped more than two hours, so
// four is headroom rather than a guess.
const gapProbeHours = 4

// StartOfDay is the first instant of the day t falls in, local to t's own
// location.
//
// Constructed from the calendar fields rather than by truncating, because
// truncation works in absolute time and a day is not always 24 hours long. In
// a zone where the clocks move, Truncate(24*time.Hour) lands an hour either
// side of midnight and every bucket after it is wrong.
//
// Some days have no midnight. Chile springs forward at 24:00, so on
// 2019-09-08 the clocks jump straight from 23:59:59 to 01:00 and 00:30 never
// happens. time.Date resolves that request *backwards*, to 23:00 on the 7th —
// which would quietly hand the previous evening to the following day's bucket.
// So the result is checked, and when it has fallen off the requested date the
// first hour that genuinely exists is used instead.
func StartOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	loc := t.Location()

	for hour := 0; hour < gapProbeHours; hour++ {
		start := time.Date(y, m, d, hour, 0, 0, 0, loc)
		if sy, sm, sd := start.Date(); sy == y && sm == m && sd == d {
			return start
		}
	}

	// Unreachable for any real zone. Returning the naive answer rather than
	// panicking: a bucket an hour off is a worse day than an outage.
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// StartOfWeek is the first instant of the Monday of the week t falls in, local
// to t's own location.
//
// Monday rather than Sunday: the application's weekly reports, check-in
// streaks and training weeks all read Monday-first, and a week that starts on
// a different day in one instrument than another is a bug nobody reports
// because it only looks slightly wrong.
//
// The Monday is reached by moving the calendar date and then asking StartOfDay
// where that day begins, rather than by subtracting days from an instant.
// Across a DST boundary the week is not 7×24 hours long, and the Monday itself
// may be one of the days that has no midnight.
func StartOfWeek(t time.Time) time.Time {
	day := StartOfDay(t)
	// Sunday is 0 in Go; shift so Monday is 0.
	offset := (int(day.Weekday()) + 6) % 7

	y, m, d := day.Date()
	// Noon on the target date: a safe anchor to hand back to StartOfDay, since
	// no transition has ever moved the clock far enough to swallow midday.
	return StartOfDay(time.Date(y, m, d-offset, 12, 0, 0, 0, day.Location()))
}
