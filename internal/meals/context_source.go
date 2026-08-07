package meals

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ContextSource reports today's logged intake versus the macro goal, plus
// the user's standing diet preferences, into Context.Nutrition.
type ContextSource struct {
	progress *TrackMealProgressService
	diets    *DietPreferenceService
}

func NewContextSource(progress *TrackMealProgressService, diets *DietPreferenceService) *ContextSource {
	return &ContextSource{progress: progress, diets: diets}
}

func (s *ContextSource) Name() string { return "nutrition" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	today := localMidnight(req.User.Location(), time.Now())

	progress, err := s.progress.ForDay(ctx, req.User.ID, today)
	if err != nil {
		// No macro goal generated yet is the normal state for a new account,
		// not a failure the coach needs to hear about.
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil
		}
		return err
	}
	into.Nutrition = append(into.Nutrition, progress.Summary())

	diets, err := s.diets.UserDiets(ctx, req.User.ID)
	if err != nil {
		return err
	}
	if len(diets) > 0 {
		names := make([]string, len(diets))
		for i, d := range diets {
			names[i] = d.Name
		}
		into.Nutrition = append(into.Nutrition, fmt.Sprintf("Diet: %s", strings.Join(names, ", ")))
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
