package workouts

import (
	"context"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Missed is a session planned for yesterday that was not trained, and where
// it could still go this week.
type Missed struct {
	// Weekday is yesterday's, and Title the session's focus (or the weekday).
	Weekday string
	Title   string
	Session SessionRef

	// TodayFree and TomorrowFree say whether that day this week has no
	// session of its own, so the missed one can move there.
	TodayFree    bool
	TomorrowFree bool
}

// Move targets for a missed session.
const (
	MoveToday    = "today"
	MoveTomorrow = "tomorrow"
)

// MissedYesterday is yesterday's planned session when it went untrained. Only
// within the current week: a Sunday missed on a Monday is already carried by
// the rotation into the new week, and there is nothing to move.
func (s *Service) MissedYesterday(ctx context.Context, user users.User, now time.Time) (Missed, bool, error) {
	now = now.In(user.Location())
	progress, err := s.WeekProgress(ctx, user, now)
	if err != nil {
		return Missed{}, false, err
	}
	yesterday := now.AddDate(0, 0, -1)
	if yesterday.Before(progress.Start) {
		return Missed{}, false, nil
	}
	missed := dayFor(progress.Days, yesterday.Weekday().String())
	if missed.Weekday == "" || missed.Completed || progress.Done(missed.Weekday) {
		return Missed{}, false, nil
	}

	out := Missed{
		Weekday: missed.Weekday,
		Title:   missed.Day.Focus,
		Session: SessionRef{PlanID: missed.PlanID, DayIndex: missed.DayIndex},
	}
	if out.Title == "" {
		out.Title = missed.Weekday
	}
	today := now.Weekday().String()
	out.TodayFree = dayFor(progress.Days, today).Weekday == "" && !progress.Done(today)
	if tomorrow := now.AddDate(0, 0, 1); tomorrow.Before(progress.Start.AddDate(0, 0, 7)) {
		out.TomorrowFree = dayFor(progress.Days, tomorrow.Weekday().String()).Weekday == ""
	}
	return out, true, nil
}

// MoveMissed puts yesterday's missed session on today or tomorrow, leaving
// every other day of the week exactly as it was.
//
// Every remaining day is pinned to the session it already has. Left unpinned,
// SetWeek would refill them from the rotation, and the rotation still points
// at the missed session (it was never trained), so it would land twice.
func (s *Service) MoveMissed(ctx context.Context, user users.User, now time.Time, to string) (WeekProgress, error) {
	now = now.In(user.Location())
	missed, ok, err := s.MissedYesterday(ctx, user, now)
	if err != nil {
		return WeekProgress{}, err
	}
	if !ok {
		return WeekProgress{}, apperr.Wrap(apperr.ErrValidation, "no session was missed yesterday")
	}
	var target time.Time
	switch {
	case to == MoveToday && missed.TodayFree:
		target = now
	case to == MoveTomorrow && missed.TomorrowFree:
		target = now.AddDate(0, 0, 1)
	default:
		return WeekProgress{}, apperr.Wrap(apperr.ErrValidation, "%q is not a free day to move the session to", to)
	}

	progress, err := s.WeekProgress(ctx, user, now)
	if err != nil {
		return WeekProgress{}, err
	}
	today := startOfLocalDay(now)
	assign := map[string]SessionRef{}
	var weekdays []string
	for _, d := range progress.Days {
		if d.Date.Before(today) || d.Completed {
			continue // history; SetWeek keeps what was trained
		}
		assign[d.Weekday] = SessionRef{PlanID: d.PlanID, DayIndex: d.DayIndex}
		weekdays = append(weekdays, d.Weekday)
	}
	wd := target.Weekday().String()
	assign[wd] = missed.Session
	weekdays = append(weekdays, wd)
	return s.SetWeek(ctx, user, now, WeekChange{Weekdays: weekdays, Assign: assign})
}

func startOfLocalDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
