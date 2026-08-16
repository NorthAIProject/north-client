package activity

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// RouteLookup reports the distance and climb a GPS provider recorded over a
// window. Satisfied by *strava.Service.
//
// Declared here rather than imported because internal/fitness/strava already
// imports this package, so depending on it directly would be a cycle. This is
// the small-local-interface pattern DOMAIN.md describes for cross-slice reads.
type RouteLookup interface {
	RouteTotals(ctx context.Context, userID uuid.UUID, since, until time.Time) (RouteTotals, error)
}

// ContextSource reports today's activity burn and the training week to the
// coach. It shares Context.FitnessSummary with the calculator's source: both
// are "numbers about the body," and a separate heading for each would be noise.
type ContextSource struct {
	svc *Service

	// routes is nil when no GPS provider is wired, which is the normal state
	// for a manual logger and for the MCP server, and means the summary simply
	// carries no distance.
	routes RouteLookup
}

func NewContextSource(svc *Service, routes RouteLookup) *ContextSource {
	return &ContextSource{svc: svc, routes: routes}
}

func (s *ContextSource) Name() string { return "activity" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	loc := req.User.Location()

	total, err := s.svc.TotalCaloriesSince(ctx, req.User.ID, localMidnight(loc, time.Now()))
	if err != nil {
		return err
	}
	if total > 0 {
		into.FitnessSummary = append(into.FitnessSummary, fmt.Sprintf("~%.0f kcal burned from logged activity today", total))
	}

	week, err := s.trainingWeek(ctx, req.User.ID, loc)
	if err != nil {
		return err
	}
	into.FitnessSummary = append(into.FitnessSummary, week.Summary()...)

	return nil
}

// trainingWeek rolls the last seven days up, or reports an empty window when
// there is nothing to roll up.
//
// Someone who has logged nothing gets no training lines at all rather than a
// row of zeroes: the coach already has "not calculated yet" under this heading
// for an empty account, and "0 sessions, 7 rest days" invites a lecture nobody
// asked for on a week that may simply not have been logged here.
func (s *ContextSource) trainingWeek(ctx context.Context, userID uuid.UUID, loc *time.Location) (TrainingWindow, error) {
	rg := timerange.Parse(timerange.KeyWeek, loc)

	sessions, err := s.svc.ListBetween(ctx, userID, rg)
	if err != nil {
		return TrainingWindow{}, err
	}
	if len(sessions) == 0 {
		return TrainingWindow{}, nil
	}

	window := NewTrainingWindow(sessions, rg.Since, rg.Days(), loc)

	prior, err := s.svc.CaloriesBetween(ctx, userID, rg.Previous())
	if err != nil {
		return TrainingWindow{}, err
	}
	window.PriorCalories = prior

	window.Route = s.routeTotals(ctx, userID, rg)

	return window, nil
}

// routeTotals is best-effort, and the one place this source swallows an error
// rather than returning it.
//
// The builder's fail-soft is all-or-nothing per source: returning here would
// cost the user their whole training summary to save a line about distance.
// Losing the distance is the smaller loss, so it is taken deliberately and
// logged rather than propagated.
func (s *ContextSource) routeTotals(ctx context.Context, userID uuid.UUID, rg timerange.Range) *RouteTotals {
	if s.routes == nil {
		return nil
	}

	totals, err := s.routes.RouteTotals(ctx, userID, rg.Since, rg.Until)
	if err != nil {
		middleware.FromContext(ctx).Warn("activity context: route totals unavailable",
			slog.Any("error", err),
		)
		return nil
	}
	if totals.Activities == 0 {
		return nil
	}
	return &totals
}

var _ coach.ContextSource = (*ContextSource)(nil)

// localMidnight is the start of the user's calendar day. Duplicated in each
// feature package that needs it (rather than importing checkins.LocalDate),
// so feature slices stay independent of one another.
func localMidnight(loc *time.Location, at time.Time) time.Time {
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
