package medications

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/medications/medication"
)

// ContextSource tells the coach which doses today were taken, skipped, or are
// still due. Under DailySignals, beside water and supplements: it is a daily
// routine, read as background.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "medications" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	day, err := s.svc.Today(ctx, req.User)
	if err != nil {
		return err
	}
	// Silent for someone who tracks none. Unlike water, having no medication
	// is the ordinary case, and a line saying so on every turn is noise.
	if day.Empty() {
		return nil
	}
	into.DailySignals = append(into.DailySignals, medication.Summary(day))
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
