package memories

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource puts approved profile facts in front of the coach.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "memories" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	list, err := s.svc.ForContext(ctx, req.User.ID)
	if err != nil {
		return err
	}
	for _, m := range list {
		into.Memories = append(into.Memories, m.Summary())
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
