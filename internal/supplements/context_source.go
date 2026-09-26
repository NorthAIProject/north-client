package supplements

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/supplements/supplement"
)

// ContextSource tells the coach what was taken today. Under DailySignals: it
// is a daily routine, read beside water and caffeine.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "supplements" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	entries, err := s.svc.Today(ctx, req.User)
	if err != nil {
		return err
	}
	into.DailySignals = append(into.DailySignals, supplement.Summary(entries))
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
