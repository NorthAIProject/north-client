package goals

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource puts the user's active goals in front of the coach on every
// message.
//
// This is the one that matters most. Without it the coach can only react to
// what was just said; with it, it can notice that what was just said has
// nothing to do with what the person told it they were working toward.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "goals" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	active, err := s.svc.ListActive(ctx, req.User.ID)
	if err != nil {
		return err
	}

	for _, goal := range active {
		into.Goals = append(into.Goals, goal.Summary())
	}

	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
