package horizon

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
)

// ContextSource tells the coach where the person's main lifts and weight are
// heading, so talk about progress starts from their own trend and its range
// rather than from a guess. The detail is one tool call away (get_forecast).
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "horizon" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	h, err := s.svc.Forecast(ctx, req.User, forecast.Scenario{})
	if err != nil {
		return err
	}
	if brief := Brief(h, false); brief != "" {
		into.FitnessSummary = append(into.FitnessSummary, brief)
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
