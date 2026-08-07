package preferences

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource puts the user's standing preferences in front of the coach,
// so its suggestions default to what they've already told the app about
// themselves rather than asking every time.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "preferences" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	p, err := s.svc.Get(ctx, req.User.ID)
	if err != nil {
		return err
	}
	into.Preferences = append(into.Preferences, p.Summary())
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
