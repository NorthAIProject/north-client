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
	stored, err := s.svc.LatestPlan(ctx, req.User.ID)
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
	progress, err := s.svc.WeekProgress(ctx, req.User, stored.Plan, now)
	if err != nil {
		return err
	}
	into.WorkoutPlan = WeekStatus(stored.Plan, progress, now) + "\n\nFull program:\n" + stored.Plan.Summary()
	return nil
}

// WeekStatus is the plan's week in two sentences: what is finished, and
// what comes next. It is how the coach knows "you already trained today"
// without the person saying so.
func WeekStatus(p Plan, progress WeekProgress, now time.Time) string {
	var b strings.Builder
	b.WriteString("This week: ")
	var done []string
	for _, d := range p.Days {
		if progress.Done(d.Weekday) {
			done = append(done, dayLabel(d)+" COMPLETED")
		}
	}
	if len(done) == 0 {
		b.WriteString("no plan day completed yet.")
	} else {
		b.WriteString(strings.Join(done, "; ") + ".")
	}
	if !progress.HasNext {
		return b.String()
	}
	b.WriteString(" Next: " + dayLabel(progress.Next))
	switch {
	case nextWeek(progress, now):
		b.WriteString(", next week — every session this week is done.")
	case strings.EqualFold(progress.Next.Weekday, now.Weekday().String()):
		b.WriteString(", today, PENDING.")
	default:
		b.WriteString(".")
	}
	return b.String()
}

func dayLabel(d PlanDay) string {
	if d.Focus == "" {
		return d.Weekday
	}
	return fmt.Sprintf("%s (%s)", d.Weekday, d.Focus)
}

// nextWeek reports whether the next open day falls after Sunday.
func nextWeek(progress WeekProgress, now time.Time) bool {
	next, ok := weekdayIndex(progress.Next.Weekday)
	if !ok {
		return false
	}
	today, _ := weekdayIndex(now.Weekday().String())
	return next < today || (next == today && progress.Done(progress.Next.Weekday))
}

// weekdayIndex counts from Monday: Monday is 0, Sunday 6.
func weekdayIndex(label string) (int, bool) {
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), strings.TrimSpace(label)) {
			return (int(d) + 6) % 7, true
		}
	}
	return 0, false
}

var _ coach.ContextSource = (*ContextSource)(nil)
