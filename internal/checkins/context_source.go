package checkins

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource puts recent daily reflections in front of the coach.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "check-ins" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	list, err := s.svc.RecentForContext(ctx, req.User)
	if err != nil {
		return err
	}
	for _, c := range list {
		into.CheckIns = append(into.CheckIns, c.Summary())
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
