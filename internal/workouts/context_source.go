package workouts

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ContextSource lets the chat coach see the plan the user is actually
// following, so it can answer "what am I doing on Wednesday?" instead of asking
// them to describe their own programme back to it.
//
// This is the pattern every future slice uses to become part of the coach's
// memory: implement Collect, register it in main. The ContextBuilder never
// changes.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource {
	return &ContextSource{svc: svc}
}

func (s *ContextSource) Name() string { return "workouts" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	stored, err := s.svc.LatestPlan(ctx, req.User.ID)
	if err != nil {
		// No plan yet is the normal state for a new account, not a failure. The
		// context renderer already says "none yet", which is what tells the
		// model there is nothing rather than leaving it to assume.
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil
		}
		return err
	}

	into.WorkoutPlan = stored.Plan.Summary()
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
