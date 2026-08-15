package health

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource reports the last week of device readings to the coach, sharing
// Context.DailySignals with the sleep and hydration sources.
//
// It belongs there rather than in FitnessSummary because these are ambient
// numbers about the body at rest — what a coach reads before interpreting
// anything else — not the training and macro figures FitnessSummary carries.
type ContextSource struct {
	svc *Service

	// now is injected so a test can assert on a fixed window. Nil means the
	// real clock, which is what production passes.
	now func() time.Time
}

func NewContextSource(svc *Service, now func() time.Time) *ContextSource {
	return &ContextSource{svc: svc, now: now}
}

func (s *ContextSource) Name() string { return "health" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	now := time.Now
	if s.now != nil {
		now = s.now
	}

	lines, err := s.svc.Summary(ctx, req.User.ID, now(), defaultSummaryDays)
	if err != nil {
		return err
	}

	// Appended without a heading of its own when empty, so an account with no
	// device contributes literally nothing rather than an empty section.
	into.DailySignals = append(into.DailySignals, lines...)
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
