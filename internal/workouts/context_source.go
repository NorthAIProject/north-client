package workouts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ContextSource lets the chat coach see the plan the user is actually
// following, so it can answer "what am I doing on Wednesday?" instead of asking
// them to describe their own programme back to it.
//
// This is the pattern every future slice uses to become part of the coach's
// memory: implement Collect, register it in main. The ContextBuilder never
// changes.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource {
	return &ContextSource{svc: svc}
}

func (s *ContextSource) Name() string { return "workouts" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	stored, err := s.svc.ActivePlan(ctx, req.User.ID)
	if err != nil {
		// No plan yet is the normal state for a new account, not a failure. The
		// context renderer already says "none yet", which is what tells the
		// model there is nothing rather than leaving it to assume.
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil
		}
		return err
	}

	now := time.Now().In(req.User.Location())
	progress, err := s.svc.WeekProgress(ctx, req.User, now)
	if err != nil {
		return err
	}
	into.WorkoutPlan = WeekStatus(progress, now) + "\n\nFull program:\n" + stored.Plan.Summary()
	return nil
}

// WeekStatus is the week in a few sentences: what is finished, what comes
// next, and — when someone changed the week — which days it trains. It is
// how the coach knows "you already trained today" without the person saying
// so, and that this is a three-day week without being told twice.
func WeekStatus(progress WeekProgress, now time.Time) string {
	var b strings.Builder
	b.WriteString("This week: ")
	var done []string
	for _, d := range progress.Days {
		if d.Completed {
			done = append(done, dayLabel(d.Day)+" COMPLETED")
		}
	}
	if len(done) == 0 {
		b.WriteString("no plan day completed yet.")
	} else {
		b.WriteString(strings.Join(done, "; ") + ".")
	}
	if progress.Custom {
		b.WriteString(" " + customWeek(progress))
	}
	if !progress.HasNext {
		return b.String()
	}
	b.WriteString(" Next: " + dayLabel(progress.Next))
	switch {
	case !progress.NextDay.Date.Before(progress.Start.AddDate(0, 0, 7)):
		b.WriteString(", next week — every session this week is done.")
	case sameDay(progress.NextDay.Date, now):
		b.WriteString(", today, PENDING.")
	default:
		b.WriteString(".")
	}
	return b.String()
}

// customWeek says which days a changed week trains.
func customWeek(progress WeekProgress) string {
	if len(progress.Days) == 0 {
		return "They made this a rest week."
	}
	labels := make([]string, 0, len(progress.Days))
	for _, d := range progress.Days {
		labels = append(labels, dayLabel(d.Day))
	}
	return fmt.Sprintf("They changed this week to %d training days: %s.", len(progress.Days), strings.Join(labels, ", "))
}

func dayLabel(d PlanDay) string {
	if d.Focus == "" {
		return d.Weekday
	}
	return fmt.Sprintf("%s (%s)", d.Weekday, d.Focus)
}

func sameDay(a, b time.Time) bool {
	b = b.In(a.Location())
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

var _ coach.ContextSource = (*ContextSource)(nil)
