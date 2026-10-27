package checkinreply

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource tells the coach what the note under today's check-in said,
// so a conversation started from it does not contradict it. It only reads a
// stored note; the coach's context never pays for one.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "check-in-reply" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	reply, ok, err := s.svc.Stored(ctx, req.User)
	if err != nil || !ok {
		return err
	}
	into.CheckIns = append(into.CheckIns, "Note already shown under today's check-in: "+reply.Text)
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
