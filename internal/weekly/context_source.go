package weekly

import (
	"context"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// ContextSource gives the coach this week's focus, so "what should I do
// today?" is answered against what the person chose on Sunday rather than
// against every goal at once.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "weekly" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	f, err := s.svc.Current(ctx, req.User, time.Now())
	if err != nil || f == nil {
		return err
	}
	into.WeekFocus = Describe(*f)
	return nil
}

// Describe is a focus as lines for a prompt.
func Describe(f Focus) []string {
	var out []string
	if len(f.Priorities) > 0 {
		out = append(out, "Priorities: "+strings.Join(f.Priorities, "; "))
	}
	switch f.Volume {
	case plan.VolumeDeload:
		out = append(out, "Training: deload week, about 60% of the plan's sets")
	case plan.VolumeBuild:
		out = append(out, "Training: build week, one extra set on each day's first two exercises")
	default:
		out = append(out, "Training: the plan as written")
	}
	return out
}

// FocusLines is the focus chosen for the week starting weekStart, for the
// weekly report; nil when that week was not reviewed. It satisfies
// reports.FocusSource.
func (s *Service) FocusLines(ctx context.Context, user users.User, weekStart time.Time) ([]string, error) {
	f, err := s.Current(ctx, user, weekStart)
	if err != nil || f == nil {
		return nil, err
	}
	return Describe(*f), nil
}
