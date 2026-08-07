package sleep

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource reports the recent sleep trend and last night to the coach,
// sharing Context.DailySignals with the hydration source.
//
// Both the trend and last night are sent: the trend is what a week is judged
// against, and last night is what explains today.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "sleep" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	trend, err := s.svc.RecentTrend(ctx, req.User, contextNights)
	if err != nil {
		return err
	}
	into.DailySignals = append(into.DailySignals, trend.Summary())

	last, ok, err := s.svc.Today(ctx, req.User)
	if err != nil {
		return err
	}
	if ok {
		into.DailySignals = append(into.DailySignals, "Last night — "+last.Summary())
	}

	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
