package mind

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource reports a mood trend (from check-ins) and recent journal
// entries into Context.Reflections — visibility into how someone's been
// feeling beyond the structured check-in fields.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "mind" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	trend, err := s.svc.RecentMoodTrend(ctx, req.User.ID, 14)
	if err != nil {
		return err
	}
	if trend.Count > 0 {
		into.Reflections = append(into.Reflections, trend.Summary())
	}

	entries, err := s.svc.Recent(ctx, req.User.ID, contextEntries)
	if err != nil {
		return err
	}
	for _, e := range entries {
		into.Reflections = append(into.Reflections, e.Summary())
	}

	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
