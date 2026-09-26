package fasting

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// ContextSource tells the coach about the current or most recent fast, under
// DailySignals with the other ambient numbers of the day.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "fasting" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	now := time.Now()
	week := timerange.Parse(timerange.KeyWeek, req.User.Location())
	fasts, err := s.svc.Overlapping(ctx, req.User, week)
	if err != nil {
		return err
	}
	// Silent when someone does not fast at all: most people do not, and a
	// "no fasts this week" line would read as a nudge to start.
	if len(fasts) > 0 {
		into.DailySignals = append(into.DailySignals, fasts[0].Summary(now))
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
