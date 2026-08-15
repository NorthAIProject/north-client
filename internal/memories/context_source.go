package memories

import (
	"context"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextCharBudget is the hard cap, in runes of Summary() text, on how much
// memory the coach is allowed to see in one turn.
//
// Twenty 240-character facts would otherwise dump ~5k characters into every
// system prompt. Pinned facts claim the budget first because the list from
// ForContext is already pinned-then-ranked (or pinned-then-newest).
const ContextCharBudget = 2000

// ContextSource puts approved profile facts in front of the coach.
//
// Which facts depends on what the user just said: the source ranks against
// req.Query and falls back to the newest facts only when there is no query to
// rank against. Pinned facts are included either way — see
// SearchApprovedForContext. Collect then clips to ContextCharBudget.
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
	for _, m := range clipToBudget(list, ContextCharBudget) {
		into.Memories = append(into.Memories, coach.Evidence{
			Ref:     coach.MemoryRef(m.ID),
			Text:    m.Summary(),
			Label:   "profile fact",
			Snippet: m.Snippet,
		})
	}
	return nil
}

// clipToBudget keeps whole facts until the rune budget is spent.
//
// Facts are never truncated: a half-sentence in the prompt is worse than an
// omitted one. A fact that does not fit is skipped so a later shorter one can
// still take the leftover. Pinned facts arrive first, so they get first claim.
func clipToBudget(list []Retrieved, budget int) []Retrieved {
	if budget <= 0 || len(list) == 0 {
		return nil
	}
	out := make([]Retrieved, 0, len(list))
	used := 0
	for _, m := range list {
		n := utf8.RuneCountInString(m.Summary())
		if n > budget && len(out) == 0 && used == 0 {
			// A single fact larger than the whole budget still goes in.
			// Validate caps content at 240, so this is defensive only.
			out = append(out, m)
			break
		}
		if used+n > budget {
			continue
		}
		out = append(out, m)
		used += n
	}
	return out
}

var _ coach.ContextSource = (*ContextSource)(nil)
