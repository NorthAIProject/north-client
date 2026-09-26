package milestones

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource lists the trackers that are due, so the coach can mention a
// dentist visit that is overdue. Ones not yet due are left out: "four months
// since the haircut" is not something to bring up unprompted.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "milestones" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	list, err := s.svc.List(ctx, req.User)
	if err != nil {
		return err
	}
	now := time.Now().In(req.User.Location())
	for _, t := range list {
		if t.Due(now) {
			into.DailySignals = append(into.DailySignals, t.Summary(now))
		}
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
