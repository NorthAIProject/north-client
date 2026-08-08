package documents

import (
	"context"
	"errors"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/search"
)

// ContextSource puts the passages that bear on this turn in front of the coach.
//
// This is what Context.KnowledgeHits was declared for. The field has been
// rendered by the prompt builder and written by nothing since it was added; a
// coach that could be handed a person's own training log has been one source
// away the whole time.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "knowledge" }

// Collect retrieves against req.Query and contributes nothing without one.
//
// The alternative — falling back to the most recent documents — would fill the
// context with whatever the person happened to upload last, every turn,
// regardless of what they asked. Retrieval that ignores the question is not
// retrieval; it is padding, and it crowds out the things that did match.
func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	hits, err := s.svc.Search(ctx, req.User.ID, req.Query, 0)
	if err != nil {
		if errors.Is(err, search.ErrEmptyTerm) {
			return nil
		}
		return err
	}

	for _, h := range hits {
		into.KnowledgeHits = append(into.KnowledgeHits, coach.Evidence{
			Ref:     coach.ChunkRef(h.ChunkID),
			Text:    h.Content,
			Label:   h.Label(),
			Snippet: h.Snippet,
		})
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
