package decisions

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource puts relevant recorded decisions in front of the coach.
//
// Which ones depends on what the user just said: Collect ranks against
// req.Query and falls back to the newest calls when there is nothing to
// match. See Service.ForContext.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "decisions" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	list, err := s.svc.ForContext(ctx, req.User.ID, req.Query)
	if err != nil {
		return err
	}
	for _, d := range list {
		into.Decisions = append(into.Decisions, d.Summary())
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
