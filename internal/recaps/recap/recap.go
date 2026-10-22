// Package recap holds the recap types, apart from internal/recaps so web
// templates can name them without importing the service that builds them.
package recap

import (
	"time"

	"github.com/google/uuid"
)

// Keys are the two windows a recap covers, both rolling and ending today.
const (
	KeyWeek  = "week"
	KeyMonth = "month"
)

// Recap is what somebody did over the last 7 or 30 days, next to the same
// span just before it.
type Recap struct {
	Key string
	// Since is the first day and Until the last, both inclusive and at
	// midnight in the person's zone.
	Since time.Time
	Until time.Time

	Totals Totals
	Prior  Totals
	// CheckInStreak is the streak as of today, not a total over the window.
	CheckInStreak int

	// Days is every day in the window, oldest first, empty days included so
	// a chart keeps its shape.
	Days []Day
	// Highlights are the person's own achievements in the window, newest
	// first.
	Highlights []Highlight
}

// Totals counts one window.
type Totals struct {
	// ActiveDays are days with at least one of the four below.
	ActiveDays int
	Workouts   int
	Sets       int
	FoodLogs   int
	CheckIns   int
}

// Day is one column of the daily chart.
type Day struct {
	Date     time.Time
	Workouts int
	Sets     int
	FoodLogs int
	CheckIns int
}

// Active is whether anything was logged that day.
func (d Day) Active() bool {
	return d.Workouts+d.Sets+d.FoodLogs+d.CheckIns > 0
}

// Highlight is one achievement shown in a recap.
type Highlight struct {
	Kind       string
	Title      string
	Detail     string
	OccurredAt time.Time
}

// Shared is a recap posted to the friends feed.
type Shared struct {
	ID uuid.UUID
	// TrainingShared is whether followers can see it: a recap sits in the
	// training category, which is on by default for new people.
	TrainingShared bool
}
