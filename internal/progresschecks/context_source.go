package progresschecks

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
)

// summaryChecks is how far back the coach's trend line reaches.
const summaryChecks = 6

// ContextSource gives the coach one line: the latest progress check and how
// each measurement moved since the oldest of the last few. Under
// FitnessSummary, beside weight and training: it is the body's trend.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "progress-checks" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	checks, err := s.svc.List(ctx, req.User, summaryChecks)
	if err != nil {
		return err
	}
	// Silent for someone who never checked: most people, most of the time.
	if line := progresscheck.Summary(checks); line != "" {
		into.FitnessSummary = append(into.FitnessSummary, line)
	}
	cmp, _, ok, err := s.svc.SavedComparison(ctx, req.User)
	if err != nil {
		return err
	}
	if ok {
		into.FitnessSummary = append(into.FitnessSummary, cmp.CoachLine())
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
