package screentime

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource reports yesterday's screen time — today's is not in yet — under
// DailySignals. Silent when none was recorded: most people will never log it.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "screentime" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	now := time.Now()
	for _, d := range []time.Time{now, now.AddDate(0, 0, -1)} {
		day, ok, err := s.svc.ForDate(ctx, req.User, d)
		if err != nil {
			return err
		}
		if ok {
			into.DailySignals = append(into.DailySignals, day.Summary())
			return nil
		}
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
