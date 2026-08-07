package calculator

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ContextSource puts the user's current calorie/macro target in front of the
// coach, so it can talk about food and training relative to an actual number
// rather than in vague terms.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "calculator" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	plan, err := s.svc.Current(ctx, req.User.ID)
	if err != nil {
		// No plan yet is the normal state for a new account, not a failure the
		// coach needs to hear about.
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil
		}
		return err
	}

	into.FitnessSummary = append(into.FitnessSummary, plan.Summary())
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
