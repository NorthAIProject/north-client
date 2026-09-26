package caffeine

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/caffeine/caffeine"
	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource tells the coach how much caffeine is on board. It shares
// DailySignals with water and sleep: the three are read together, and
// caffeine late in the day is most of what a bad night's sleep is about.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "caffeine" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	entries, err := s.svc.Today(ctx, req.User)
	if err != nil {
		return err
	}
	into.DailySignals = append(into.DailySignals, caffeine.Summary(entries, time.Now()))
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
