package hydration

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource reports today's water intake to the coach. It shares
// Context.DailySignals with the sleep source: both are ambient daily numbers,
// and a heading each would make the prompt mostly headings.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "hydration" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	day, err := s.svc.Today(ctx, req.User)
	if err != nil {
		return err
	}

	// Reported even when nothing is logged: "drank nothing today" is a fact
	// the coach should be able to act on, and staying silent would let it
	// assume the day was fine.
	into.DailySignals = append(into.DailySignals, day.Summary())
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
