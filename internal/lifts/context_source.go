package lifts

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// ContextSource tells the coach what has been lifted lately and which
// records fell, so advice on load and progression starts from the numbers.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "lifts" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	sets, records, err := s.svc.Recent(ctx, req.User)
	if err != nil {
		return err
	}
	into.FitnessSummary = append(into.FitnessSummary, lift.Summary(sets, records))

	recap, ok, err := s.svc.LatestRecap(ctx, req.User, timerange.Parse(timerange.KeyWeek, req.User.Location()))
	if err != nil {
		return err
	}
	if ok {
		into.FitnessSummary = append(into.FitnessSummary, lift.RecapSummary(recap, req.User.Location()))
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
