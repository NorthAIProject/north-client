package activity

import (
	"fmt"
	"strings"
	"time"
)

// PlanSlot is one day of a training plan, enough to judge whether it was done.
type PlanSlot struct {
	Weekday string
	Focus   string
}

// DayStatus is one plan day on one date.
type DayStatus struct {
	Weekday string
	Focus   string
	Date    time.Time
	Done    bool
}

// Adherence is how many plan days in a window were finished.
type Adherence struct {
	Planned  int
	Done     int
	Days     []DayStatus
	Sentence string
}

// CanonicalWeekday returns the English weekday name, title case, as the plan
// stores it. Anything else is refused so a typo cannot mark a day done.
func CanonicalWeekday(label string) (string, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", false
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), label) {
			return d.String(), true
		}
	}
	return "", false
}

// IsStrength reports a resistance session. A run logged on leg day is not
// the plan's workout.
func IsStrength(code string) bool {
	return strings.HasPrefix(code, "strength_training")
}

// WeekStart is local midnight on the Monday that begins t's week — the same
// Monday-to-Monday week the weekly review covers. A plan day is done for
// this week only; next Monday every day is open again.
func WeekStart(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	day := startOfDay(t, loc)
	back := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -back)
}

// completion is one plan occurrence a session finished: that weekday, in the
// week that starts on week.
type completion struct {
	week    time.Time
	weekday string
}

// completionOf reports which plan day a session finished. A session that
// names its plan day counts for that day of the week it started in, so
// Wednesday's workout done on Tuesday still finishes Wednesday. A strength
// session that names none counts for the weekday it started on. Cancelled
// and open sessions finish nothing, and neither does an unnamed run.
func completionOf(s Session, loc *time.Location) (completion, bool) {
	if s.Status != StatusCompleted {
		return completion{}, false
	}
	week := WeekStart(s.StartedAt, loc)
	if weekday, ok := CanonicalWeekday(s.PlanWeekday); ok {
		return completion{week: week, weekday: weekday}, true
	}
	if !IsStrength(s.ActivityCode) {
		return completion{}, false
	}
	return completion{week: week, weekday: s.StartedAt.In(loc).Weekday().String()}, true
}

func completions(sessions []Session, loc *time.Location) map[completion]bool {
	out := make(map[completion]bool, len(sessions))
	for _, s := range sessions {
		if c, ok := completionOf(s, loc); ok {
			out[c] = true
		}
	}
	return out
}

// CompletedWeekdays lists the weekdays finished in the week containing now,
// Monday first. sessions should cover at least that week.
func CompletedWeekdays(sessions []Session, loc *time.Location, now time.Time) []string {
	if loc == nil {
		loc = time.UTC
	}
	done := completions(sessions, loc)
	week := WeekStart(now, loc)
	var names []string
	for i := 0; i < 7; i++ {
		name := week.AddDate(0, 0, i).Weekday().String()
		if done[completion{week: week, weekday: name}] {
			names = append(names, name)
		}
	}
	return names
}

// ThisWeek is the plan laid over the Monday–Sunday week containing now: one
// row per plan day in date order, each done or still open.
func ThisWeek(slots []PlanSlot, sessions []Session, loc *time.Location, now time.Time) Adherence {
	if loc == nil {
		loc = time.UTC
	}
	week := WeekStart(now, loc)
	out := adherence(slots, sessions, loc, week, week.AddDate(0, 0, 7), true)
	out.Sentence = weekSentence(out.Done, out.Planned)
	return out
}

// PlanAdherence counts plan days whose date falls in [since, until) and how
// many of those were finished. It is a rate, not a checklist, so Days is
// left empty; ThisWeek is the checklist.
func PlanAdherence(slots []PlanSlot, sessions []Session, loc *time.Location, since, until time.Time) Adherence {
	if loc == nil {
		loc = time.UTC
	}
	out := adherence(slots, sessions, loc, startOfDay(since, loc), startOfDay(until, loc), false)
	if out.Planned > 0 {
		out.Sentence = fmt.Sprintf("%d of %s done.", out.Done, plural(out.Planned, "planned session"))
	}
	return out
}

func adherence(slots []PlanSlot, sessions []Session, loc *time.Location, since, until time.Time, withDays bool) Adherence {
	done := completions(sessions, loc)
	var out Adherence
	for day := since; day.Before(until); day = day.AddDate(0, 0, 1) {
		slot, ok := slotOn(slots, day.Weekday().String())
		if !ok {
			continue
		}
		finished := done[completion{week: WeekStart(day, loc), weekday: day.Weekday().String()}]
		out.Planned++
		if finished {
			out.Done++
		}
		if withDays {
			out.Days = append(out.Days, DayStatus{Weekday: slot.Weekday, Focus: slot.Focus, Date: day, Done: finished})
		}
	}
	return out
}

func weekSentence(done, planned int) string {
	switch {
	case planned == 0:
		return ""
	case planned == 1 && done == 1:
		return "This week's session is done."
	case done == planned:
		return fmt.Sprintf("All %s done this week.", plural(planned, "session"))
	default:
		return fmt.Sprintf("%d of %s done this week.", done, plural(planned, "session"))
	}
}

func slotOn(slots []PlanSlot, weekday string) (PlanSlot, bool) {
	for _, s := range slots {
		if strings.EqualFold(strings.TrimSpace(s.Weekday), weekday) {
			return s, true
		}
	}
	return PlanSlot{}, false
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
