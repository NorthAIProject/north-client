package nudges

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/users"
)

// progressSource answers whether a progress check is due today.
// progresschecks.Service satisfies it.
type progressSource interface {
	Due(ctx context.Context, user users.User, now time.Time) (progresscheck.Schedule, bool, error)
}

// WithProgressChecks turns on the progress-check reminder.
func (s *Service) WithProgressChecks(p progressSource) *Service {
	s.progress = p
	return s
}

// evalProgressCheck raises one quiet note when a scheduled progress check
// falls due. The dedupe key is the due date, so a window left alone is asked
// about once, not every sweep; saving a check moves the date on.
//
// The person chose the cadence, so no first-week silence applies. A skipped
// window counts against nothing.
func (s *Service) evalProgressCheck(ctx context.Context, user users.User, now time.Time) (int, error) {
	if s.progress == nil {
		return 0, nil
	}
	sched, due, err := s.progress.Due(ctx, user, now)
	if err != nil || !due {
		return 0, err
	}
	_, inserted, err := s.Raise(ctx, user, Draft{
		Kind:      KindProgressCheckDue,
		DedupeKey: sched.NextDueOn.Format("2006-01-02"),
		Title:     "Progress check",
		Body:      "Photos, weight and a waist measurement, whenever suits you today.",
		Href:      "/app/progress",
	})
	if err != nil || !inserted {
		return 0, err
	}
	return 1, nil
}
