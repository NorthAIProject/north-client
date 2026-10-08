package lifts

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// ContextSource tells the coach what has been lifted lately, which records
// fell, and which muscles are still tired or going stale, so advice on load,
// progression and what to train today starts from the numbers.
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

	load, err := s.svc.Readiness(ctx, req.User)
	if err != nil {
		return err
	}
	// Someone who has never lifted already reads "no sets" above.
	if !load.LastSession.IsZero() {
		into.FitnessSummary = append(into.FitnessSummary, lift.ReadinessSummary(load))
	}

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
