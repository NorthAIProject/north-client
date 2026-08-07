package habits

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource reports active habits with their streaks and adherence.
//
// Habits get their own heading rather than joining DailySignals because they
// are the one thing in the daily picture carrying an expectation: "kept 2 of
// 3" is a judgement the coach can act on, where "drank 1.5L" is only a fact.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "habits" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	stats, err := s.svc.Today(ctx, req.User)
	if err != nil {
		return err
	}

	for _, st := range stats {
		into.Habits = append(into.Habits, st.Summary())
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
