package soreness

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
)

// ContextSource tells the coach where it hurts today. It goes under
// FitnessSummary, beside the training plan it should change.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "soreness" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	entries, err := s.svc.OnDate(ctx, req.User, time.Now())
	if err != nil {
		return err
	}
	if line := sore.Summary(entries); line != "" {
		into.DailySignals = append(into.DailySignals, line)
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
