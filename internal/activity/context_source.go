package activity

import (
	"context"
	"fmt"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextSource reports today's logged activity burn to the coach. It shares
// Context.FitnessSummary with the calculator's source: both are "numbers
// about the body," and a separate heading for each would be noise.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "activity" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	total, err := s.svc.TotalCaloriesSince(ctx, req.User.ID, localMidnight(req.User.Location(), time.Now()))
	if err != nil {
		return err
	}
	if total > 0 {
		into.FitnessSummary = append(into.FitnessSummary, fmt.Sprintf("~%.0f kcal burned from logged activity today", total))
	}
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)

// localMidnight is the start of the user's calendar day. Duplicated in each
// feature package that needs it (rather than importing checkins.LocalDate),
// so feature slices stay independent of one another.
func localMidnight(loc *time.Location, at time.Time) time.Time {
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
