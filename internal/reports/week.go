package reports

import (
	"fmt"
	"time"
)

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

// Contains reports whether t falls in [Start, End).
func (w Week) Contains(t time.Time) bool {
	t = t.In(w.Start.Location())
	return !t.Before(w.Start) && t.Before(w.End)
}
