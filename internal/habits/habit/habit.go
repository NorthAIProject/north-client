// Package habit holds the shape of a recurring intention, its completions,
// and the adherence maths over the two.
//
// A leaf, so the habits service and any template that renders a habit do not
// import each other. See CLAUDE.md on slice layout.
//
// The streak and adherence functions live here rather than in the service
// because they are pure: given a schedule, a set of completed dates and a
// "today", the answer is arithmetic. That makes the one genuinely tricky rule
// in this slice — that an unscheduled day is not a missed day — testable
// without a database.
package habit

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxLookback bounds the backwards walk in Streak. A habit held daily for
// three years is a lovely thing and still does not need counting past a year
// to say "you have never missed".
const maxLookback = 366

// Habit is a behaviour someone intends to repeat.
type Habit struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Name string

	// Domain is a life domain from internal/shared/lifedomain.
	Domain string

	// Days is the schedule. Never empty: a habit due on no days could never
	// be missed or kept, so the service rejects it.
	Days []time.Weekday

	// Active is false for a habit someone has stopped. Archived rather than
	// deleted, since having dropped it is itself worth the coach knowing.
	Active bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ScheduledOn reports whether the habit is due on the given day.
func (h Habit) ScheduledOn(day time.Time) bool { return h.hasWeekday(day.Weekday()) }

func (h Habit) hasWeekday(w time.Weekday) bool {
	for _, d := range h.Days {
		if d == w {
			return true
		}
	}
	return false
}

// IsDaily reports a habit scheduled every day, which is worth phrasing
// differently in the UI than listing seven weekdays.
func (h Habit) IsDaily() bool { return len(h.Days) == 7 }

// Schedule renders the days for display: "Every day", "Mon, Wed, Fri".
func (h Habit) Schedule() string {
	if h.IsDaily() {
		return "Every day"
	}

	// Rendered in week order rather than storage order, so a habit stored as
	// {5,1,3} still reads "Mon, Wed, Fri".
	var names []string
	for day := time.Sunday; day <= time.Saturday; day++ {
		if h.hasWeekday(day) {
			names = append(names, day.String()[:3])
		}
	}
	return strings.Join(names, ", ")
}

// Completion is one day a habit was kept.
type Completion struct {
	ID      uuid.UUID
	HabitID uuid.UUID
	UserID  uuid.UUID

	LocalDate   time.Time
	CompletedAt time.Time
}

// Stats is a habit's recent record.
type Stats struct {
	Habit Habit

	// Streak is consecutive scheduled days kept, ending today or on the last
	// scheduled day before it.
	Streak int

	// Kept and Scheduled cover the trailing adherence window.
	Kept      int
	Scheduled int

	// DoneToday is false both when today is scheduled and not yet kept, and
	// when today is not a scheduled day at all — check ScheduledToday to
	// tell those apart before showing someone a red mark.
	DoneToday      bool
	ScheduledToday bool
}

// Rate is adherence over the window as a percentage. A window containing no
// scheduled days is 100: nothing was asked for, so nothing was missed.
func (s Stats) Rate() int {
	if s.Scheduled <= 0 {
		return 100
	}
	return s.Kept * 100 / s.Scheduled
}

// Summary renders a habit's record for the coach's context.
func (s Stats) Summary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s (%s, %s): ", s.Habit.Name, s.Habit.Domain, strings.ToLower(s.Habit.Schedule()))

	if s.Scheduled == 0 {
		b.WriteString("not due recently")
	} else {
		fmt.Fprintf(&b, "kept %d of %d (%d%%)", s.Kept, s.Scheduled, s.Rate())
	}

	switch {
	case s.Streak == 1:
		b.WriteString(", 1 day streak")
	case s.Streak > 1:
		fmt.Fprintf(&b, ", %d day streak", s.Streak)
	}

	if s.ScheduledToday && !s.DoneToday {
		b.WriteString(", due today")
	}

	return b.String()
}

// DateSet is a set of local dates, keyed so that two instants on the same
// calendar day collide regardless of their clock time or location.
type DateSet map[string]bool

func NewDateSet(dates []time.Time) DateSet {
	set := make(DateSet, len(dates))
	for _, d := range dates {
		set[DateKey(d)] = true
	}
	return set
}

func (s DateSet) Has(day time.Time) bool { return s[DateKey(day)] }

func DateKey(t time.Time) string { return t.Format("2006-01-02") }

// Streak counts consecutive scheduled days kept, walking backwards from today.
//
// The rule that makes this different from a plain consecutive-day count: a day
// the habit was not scheduled for is skipped, not counted as missed. A habit
// held every Monday, Wednesday and Friday must not lose its streak because
// Tuesday happened.
//
// Today is forgiven while it is still today: a scheduled day that has not been
// kept yet ends the walk at yesterday rather than reporting zero, because the
// day is not over. Zero therefore means "not started or genuinely broken",
// never "it is only nine in the morning".
func Streak(h Habit, completed DateSet, today time.Time) int {
	if len(h.Days) == 0 {
		return 0
	}

	day := today
	if h.ScheduledOn(day) && !completed.Has(day) {
		day = day.AddDate(0, 0, -1)
	}

	streak := 0
	for i := 0; i < maxLookback; i++ {
		if h.ScheduledOn(day) {
			if !completed.Has(day) {
				return streak
			}
			streak++
		}
		day = day.AddDate(0, 0, -1)
	}
	return streak
}

// Adherence counts scheduled days and kept days over the trailing window
// ending today.
//
// Today is counted only once it has been kept, matching Streak's forgiveness:
// a habit due at 9pm should not read as missed at 9am.
func Adherence(h Habit, completed DateSet, today time.Time, days int) (kept, scheduled int) {
	if len(h.Days) == 0 || days <= 0 {
		return 0, 0
	}

	for i := 0; i < days; i++ {
		day := today.AddDate(0, 0, -i)
		if !h.ScheduledOn(day) {
			continue
		}

		done := completed.Has(day)
		if i == 0 && !done {
			continue // today, not over yet
		}

		scheduled++
		if done {
			kept++
		}
	}
	return kept, scheduled
}
