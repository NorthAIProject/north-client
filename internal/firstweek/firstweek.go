// Package firstweek runs a new person's first seven days: one goal, one small
// daily action, and the coach carrying the week.
//
// Day 0 is the day onboarding finishes. Days 1 to 7 are the week; day 7 ends
// in a review. The day number is never stored: it is the count of local days
// since started_on, in the timezone captured at the start, so a person who
// flies mid-week neither skips nor repeats a day.
//
// A day counts as done when the template's proof finds the matching log (a
// sleep entry, a workout, a meal, a journal entry, a check-in) or when the
// person taps Done. first_week_days records it once, which is what lets
// first_week_day_completed fire once.
package firstweek

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// Days is the length of the programme.
const Days = 7

// abandonAfter is how many days past day 7 an unfinished week stays active
// before the sweep calls it abandoned.
const abandonAfter = 3

// Templates. They travel as open strings on the wire.
const (
	TemplateSleep   = "sleep"
	TemplateTrain   = "train"
	TemplateProtein = "protein"
	TemplateJournal = "journal"
	TemplateCustom  = "custom"
)

// Templates in the order they are offered.
var Templates = []string{TemplateSleep, TemplateTrain, TemplateProtein, TemplateJournal, TemplateCustom}

// Statuses. StatusNone is never stored: it is what the API reports for a
// person with no programme.
const (
	StatusActive    = "active"
	StatusFinished  = "finished"
	StatusSkipped   = "skipped"
	StatusAbandoned = "abandoned"
	StatusNone      = "none"
)

// How a day was known to be done.
const (
	ViaLog = "log"
	ViaTap = "tap"
)

// NormalizeTemplate trims and checks a template name.
func NormalizeTemplate(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	for _, t := range Templates {
		if raw == t {
			return t, true
		}
	}
	return "", false
}

// category is the goal category a template's goal is filed under.
func category(template string) string {
	switch template {
	case TemplateTrain:
		return lifedomain.Fitness
	case TemplateSleep, TemplateProtein:
		return lifedomain.Health
	default:
		return lifedomain.Personal
	}
}

// Programme is one person's week.
type Programme struct {
	UserID     uuid.UUID
	GoalID     *uuid.UUID
	Template   string
	Timezone   string
	StartedOn  time.Time // day 0, a date at UTC midnight
	Status     string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Location is the zone the week runs in. An unreadable zone falls back to
// UTC rather than failing every read of the programme.
func (p Programme) Location() *time.Location {
	if loc, err := time.LoadLocation(p.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// Day is the programme day at now: 0 on the start date, 1 to 7 during the
// week, more after it.
func (p Programme) Day(now time.Time) int {
	local := now.In(p.Location())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	// Both are UTC midnights, so the difference is a whole number of days
	// whatever the local clocks did in between.
	return int(today.Sub(p.StartedOn).Hours() / 24)
}

// DateOf is the local date of programme day n, at UTC midnight.
func (p Programme) DateOf(day int) time.Time {
	return p.StartedOn.AddDate(0, 0, day)
}

// DayRange is programme day n as a window in the week's own zone, midnight
// to midnight. AddDate rather than 24 hours: the night the clocks change is
// 23 or 25 hours long.
func (p Programme) DayRange(day int) timerange.Range {
	d := p.DateOf(day)
	since := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, p.Location())
	return timerange.Between(since, since.AddDate(0, 0, 1))
}

// InWeek reports whether day is one of the programme's seven.
func InWeek(day int) bool { return day >= 1 && day <= Days }

// Completion is one day done.
type Completion struct {
	Day         int
	LocalDate   time.Time
	Via         string
	CompletedAt time.Time
}

// DayState is one of the seven days as the person sees it.
type DayState struct {
	Day  int
	Date time.Time
	Done bool
	Via  string
}

// Today is the week as of now.
type Today struct {
	Programme Programme
	// Day is the programme day now; see Programme.Day.
	Day int
	// Done is today's action done. Always false outside days 1 to 7.
	Done bool
	Days []DayState
}

// DoneCount is how many of the seven days are done.
func (t Today) DoneCount() int {
	n := 0
	for _, d := range t.Days {
		if d.Done {
			n++
		}
	}
	return n
}

// ThreeOfThree reports day 3 with days 1 to 3 all done: the day the card
// shows the "3 of 3 days" moment.
func (t Today) ThreeOfThree() bool {
	if t.Day != 3 || len(t.Days) < 3 {
		return false
	}
	return t.Days[0].Done && t.Days[1].Done && t.Days[2].Done
}
