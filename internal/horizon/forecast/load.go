package forecast

import (
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// PlannedExercise is one exercise of a planned session.
type PlannedExercise struct {
	// Slug is the catalog exercise; one without a slug has no muscles to
	// load and is skipped.
	Slug string
	Sets int
}

// PlannedSession is the training planned for one weekday.
type PlannedSession struct {
	Weekday   time.Weekday
	Exercises []PlannedExercise
}

// Frame is the body on one day ahead: how fatigued and how detrained each
// muscle would be if the plan is followed.
type Frame struct {
	OffsetDays int
	At         time.Time
	Load       lift.Load
}

// Deload settings for the what-if.
const (
	// deloadShare is the share of planned sets done in a lighter week.
	deloadShare = 0.6
	// sessionHour is when a planned session is assumed to happen, and
	// frameHour when each day is read: the morning after, which is when
	// somebody asks "am I recovered?".
	sessionHour = 18
	frameHour   = 8
)

// Frames runs the plan forward. It adds no physiology: the future is the
// plan written out as sets, read by the same lift.LoadOf that draws today's
// body map.
//
// history is the sets already logged. plan is the week as planned; an empty
// plan gives frames in which the person simply stops training, which is the
// truthful picture for somebody with no plan. now carries the person's
// location, which decides what weekday each day ahead is.
func Frames(history []lift.Set, plan []PlannedSession, weights lift.Weights, now time.Time, sc Scenario) []Frame {
	future := planned(plan, now, sc)
	all := make([]lift.Set, 0, len(history)+len(future))
	all = append(all, history...)
	all = append(all, future...)

	offsets := Offsets()
	out := make([]Frame, 0, len(offsets))
	for _, offset := range offsets {
		at := now
		if offset > 0 {
			at = morning(now, offset)
		}
		out = append(out, Frame{OffsetDays: offset, At: at, Load: lift.LoadOf(all, weights, at)})
	}
	return out
}

// planned writes the coming MaxOffset days of the plan out as sets.
func planned(plan []PlannedSession, now time.Time, sc Scenario) []lift.Set {
	byDay := map[time.Weekday][]PlannedExercise{}
	for _, s := range plan {
		byDay[s.Weekday] = append(byDay[s.Weekday], s.Exercises...)
	}
	extra := extraDay(byDay, sc)

	var out []lift.Set
	for offset := 0; offset <= MaxOffset; offset++ {
		if offset < sc.SkipWeeks*7 {
			continue
		}
		at := evening(now, offset)
		if !at.After(now) {
			continue // today's session time has passed; what was done is in history
		}
		exercises := byDay[at.Weekday()]
		if extra != nil && at.Weekday() == extra.day {
			exercises = extra.exercises
		}
		share := 1.0
		// The lighter week is the first week of training, which starts
		// after any time away.
		if sc.DeloadNow && offset < sc.SkipWeeks*7+7 {
			share = deloadShare
		}
		for _, e := range exercises {
			if e.Slug == "" {
				continue
			}
			n := int(float64(e.Sets)*share + 0.5)
			for i := 0; i < n; i++ {
				out = append(out, lift.Set{ExerciseSlug: e.Slug, Kind: lift.KindWork, PerformedAt: at, SetNumber: i + 1})
			}
		}
	}
	return out
}

type extraSession struct {
	day       time.Weekday
	exercises []PlannedExercise
}

// extraDay is the what-if's added session: a repeat of the week's first
// session, on the rest day furthest from it. Nil when none is asked for,
// there is no plan, or every day already has training.
func extraDay(byDay map[time.Weekday][]PlannedExercise, sc Scenario) *extraSession {
	if sc.ExtraSessions <= 0 || len(byDay) == 0 || len(byDay) >= 7 {
		return nil
	}
	// The week's first session, Monday first.
	var first time.Weekday
	found := false
	for i := 0; i < 7 && !found; i++ {
		d := time.Weekday((i + 1) % 7)
		if len(byDay[d]) > 0 {
			first, found = d, true
		}
	}
	if !found {
		return nil
	}
	best, bestGap := first, -1
	for i := 0; i < 7; i++ {
		d := time.Weekday((i + 1) % 7)
		if len(byDay[d]) > 0 {
			continue
		}
		gap := (int(d) - int(first) + 7) % 7
		if gap > 3 {
			gap = 7 - gap
		}
		if gap > bestGap {
			best, bestGap = d, gap
		}
	}
	return &extraSession{day: best, exercises: byDay[first]}
}

func evening(now time.Time, offset int) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+offset, sessionHour, 0, 0, 0, now.Location())
}

func morning(now time.Time, offset int) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+offset, frameHour, 0, 0, 0, now.Location())
}
