// Package milestone holds "months since" trackers: the dentist, a haircut, a
// new toothbrush.
package milestone

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Tracker is one thing whose age is worth seeing.
type Tracker struct {
	ID             uuid.UUID
	Name           string
	LastDoneOn     time.Time
	IntervalMonths *int
}

// MonthsSince counts whole calendar months from the last time to on.
func (t Tracker) MonthsSince(on time.Time) int {
	y1, m1, d1 := t.LastDoneOn.Date()
	y2, m2, d2 := on.Date()
	months := (y2-y1)*12 + int(m2) - int(m1)
	if d2 < d1 {
		months--
	}
	return max(months, 0)
}

// Fraction is how far toward due it is, 0 when no interval is set.
func (t Tracker) Fraction(on time.Time) float64 {
	if t.IntervalMonths == nil || *t.IntervalMonths <= 0 {
		return 0
	}
	return min(float64(t.MonthsSince(on))/float64(*t.IntervalMonths), 1)
}

// Due reports whether the interval has passed.
func (t Tracker) Due(on time.Time) bool {
	return t.IntervalMonths != nil && t.MonthsSince(on) >= *t.IntervalMonths
}

// Summary renders a tracker for the coach.
func (t Tracker) Summary(on time.Time) string {
	s := fmt.Sprintf("%s: %d months since (%s)", t.Name, t.MonthsSince(on), t.LastDoneOn.Format("2 Jan 2006"))
	if t.Due(on) {
		s += ", due"
	}
	return s + "."
}
