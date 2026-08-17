package reports

import (
	"fmt"
	"time"
)

// Period is the span one report covers, already titled.
//
// Both kinds of report store a half-open [Start, End) range and a heading, and
// nothing below the service layer needs to know which kind produced it — so the
// repository takes this rather than a Week, and a daily briefing is just a
// Period one day wide.
type Period struct {
	Start time.Time
	End   time.Time
	Title string
}

// Week is a Monday–Sunday span in the user's timezone.
//
// Start is midnight Monday local. End is midnight the following Monday
// (exclusive), so a half-open window matches check-in and activity queries.
type Week struct {
	Start time.Time
	End   time.Time
}

// WeekContaining returns the local ISO-style week (Monday start) that holds at.
func WeekContaining(at time.Time, loc *time.Location) Week {
	if loc == nil {
		loc = time.UTC
	}
	t := at.In(loc)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	// Sunday is 0 in Go; shift so Monday is 0.
	offset := (int(day.Weekday()) + 6) % 7
	start := day.AddDate(0, 0, -offset)
	return Week{Start: start, End: start.AddDate(0, 0, 7)}
}

// Title is the list heading, e.g. "Week of 4 Aug 2026".
func (w Week) Title() string {
	return fmt.Sprintf("Week of %s", w.Start.Format("2 Jan 2006"))
}

// Period is the week as the repository stores it.
func (w Week) Period() Period {
	return Period{Start: w.Start, End: w.End, Title: w.Title()}
}

// Contains reports whether t falls in [Start, End).
func (w Week) Contains(t time.Time) bool {
	t = t.In(w.Start.Location())
	return !t.Before(w.Start) && t.Before(w.End)
}

// DayContaining returns the local day that holds at, as a one-day Period.
//
// The briefing is about a single morning, so unlike Week this is not aligned to
// anything: it is simply local midnight to local midnight.
func DayContaining(at time.Time, loc *time.Location) Period {
	if loc == nil {
		loc = time.UTC
	}
	t := at.In(loc)
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	return Period{
		Start: start,
		End:   start.AddDate(0, 0, 1),
		Title: fmt.Sprintf("Briefing for %s", start.Format("Monday 2 Jan")),
	}
}
