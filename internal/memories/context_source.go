package memories

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource puts approved profile facts in front of the coach.
//
// Which facts depends on what the user just said: the source ranks against
// req.Query and falls back to the newest facts only when there is no query to
// rank against. Pinned facts are included either way — see
// SearchApprovedForContext.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "memories" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	list, err := s.svc.ForContext(ctx, req.User.ID, req.Query)
	if err != nil {
		return err
	}
	for _, m := range list {
		into.Memories = append(into.Memories, coach.Evidence{
			Ref:     coach.MemoryRef(m.ID),
			Text:    m.Summary(),
			Label:   "profile fact",
			Snippet: m.Snippet,
		})
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
