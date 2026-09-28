package activity

import (
	"strings"
	"time"
)

// PlanSlot is one day of a training plan, enough to judge whether it was done.
type PlanSlot struct {
	Weekday string
	Focus   string
}

// DayStatus is one plan day inside a short window.
type DayStatus struct {
	Weekday string
	Focus   string
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

// WeekWindow is the rolling seven local days through today, half-open
// [start of today-6, start of tomorrow). Each weekday appears once, so a
// completion drops off when that weekday comes around again.
func WeekWindow(now time.Time, loc *time.Location) (since, until time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	t := now.In(loc)
	today := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	return today.AddDate(0, 0, -6), today.AddDate(0, 0, 1)
}

// CompletedWeekdayNames lists the plan weekdays finished in the rolling week
// that contains now. A session that names its plan day counts for that day
// even when it was started on another date. A strength session that names
// none counts for the weekday it started on. Cancelled and open sessions do
// not count.
func CompletedWeekdayNames(sessions []Session, loc *time.Location, now time.Time) []string {
	if loc == nil {
		loc = time.UTC
	}
	since, until := WeekWindow(now, loc)
	var names []string
	seen := map[string]bool{}
	for day := since; day.Before(until); day = day.AddDate(0, 0, 1) {
		if !completedOn(sessions, day, loc) {
			continue
		}
		name := day.Weekday().String()
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// PlanAdherence counts plan days whose date falls in [since, until) and how
// many of those were finished. Days is filled only when the window is a week
// or shorter, one row per plan day in order, because a month of the same
// weekday is a count rather than a strip.
func PlanAdherence(slots []PlanSlot, sessions []Session, loc *time.Location, since, until time.Time) Adherence {
	if loc == nil {
		loc = time.UTC
	}
	since = startOfDay(since, loc)
	until = startOfDay(until, loc)
	var out Adherence
	week := until.Sub(since) <= 8*24*time.Hour
	for day := since; day.Before(until); day = day.AddDate(0, 0, 1) {
		slot, ok := slotOn(slots, day.Weekday().String())
		if !ok {
			continue
		}
		out.Planned++
		done := completedOn(sessions, day, loc)
		if done {
			out.Done++
		}
		if week {
			out.Days = append(out.Days, DayStatus{Weekday: slot.Weekday, Focus: slot.Focus, Done: done})
		}
	}
	out.Sentence = adherenceSentence(out.Done, out.Planned, week)
	return out
}

func adherenceSentence(done, planned int, week bool) string {
	if planned == 0 {
		return ""
	}
	when := "in this window"
	if week {
		when = "this week"
	}
	noun := "sessions"
	if planned == 1 {
		noun = "session"
	}
	return strings.TrimSpace(itoa(done) + " of " + itoa(planned) + " " + noun + " done " + when + ".")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func slotOn(slots []PlanSlot, weekday string) (PlanSlot, bool) {
	for _, s := range slots {
		if strings.EqualFold(s.Weekday, weekday) {
			return s, true
		}
	}
	return PlanSlot{}, false
}

// completedOn reports whether any session finishes the plan occurrence on day.
func completedOn(sessions []Session, day time.Time, loc *time.Location) bool {
	for _, s := range sessions {
		if completes(s, day, loc) {
			return true
		}
	}
	return false
}

// completes reports whether this session is the workout for the plan
// occurrence on day. The session has to have started within the six days
// before that occurrence, so last week's Wednesday does not finish this one,
// and a Wednesday session started on Tuesday still does.
func completes(s Session, day time.Time, loc *time.Location) bool {
	if s.Status != StatusCompleted {
		return false
	}
	start := s.StartedAt.In(loc)
	dayStart := startOfDay(day, loc)
	if start.Before(dayStart.AddDate(0, 0, -6)) || !start.Before(dayStart.AddDate(0, 0, 1)) {
		return false
	}
	if canonical, ok := CanonicalWeekday(s.PlanWeekday); ok {
		return strings.EqualFold(canonical, day.Weekday().String())
	}
	if !IsStrength(s.ActivityCode) {
		return false
	}
	started := startOfDay(start, loc)
	return started.Equal(dayStart)
}

func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
