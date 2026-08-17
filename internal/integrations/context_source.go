package integrations

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource puts the next seven days of somebody's own calendar in front of
// the coach.
//
// This is the whole reason the package exists: a coach that suggests a Thursday
// session to somebody who is on a plane on Thursday is not coaching, it is
// guessing. Everything below it — the MCP client, the adapter, the sealed
// token — is in service of these few strings.
//
// Only strings cross this line. The coach never learns that MCP exists.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "calendar" }

// Collect fills the calendar section, or returns an error and fills nothing.
//
// Returning the error is the entire graceful-degradation story: ContextBuilder
// logs it, counts it as north_context_source_failed, and carries on building
// the rest of the context. An unreachable calendar costs the coach one section,
// not the reply.
func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	lines, err := s.svc.Upcoming(ctx, req.User.ID)
	if err != nil {
		return err
	}
	into.Calendar = append(into.Calendar, lines...)
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
