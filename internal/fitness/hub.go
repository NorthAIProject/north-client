package fitness

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
	fitnesspages "github.com/NorthAIProject/north-client/web/fitness"
)

const calorieWindow = 7

// Options wires the slices the fitness hub composes.
type Options struct {
	Activity *activity.Service
	Workouts *workouts.Service
	Strava   *strava.Service
	Meals    *meals.TrackMealProgressService
}

type Service struct {
	activity *activity.Service
	workouts *workouts.Service
	strava   *strava.Service
	meals    *meals.TrackMealProgressService
}

func NewService(opts Options) *Service {
	return &Service{
		activity: opts.Activity,
		workouts: opts.Workouts,
		strava:   opts.Strava,
		meals:    opts.Meals,
	}
}

// DayPoint is one label/value pair for instrument charts.
type DayPoint struct {
	Label string
	Value float64
}

// Snapshot is everything the fitness hub renders.
type Snapshot struct {
	Calories7d    float64
	CalorieSeries []DayPoint

	PlanID      uuid.UUID
	NextSession *plan.PlanDay

	StravaStatus strava.Status

	MealProgress    *meals.Progress
	HasMealProgress bool
}

func (s Snapshot) HasCalorieChart() bool {
	for _, d := range s.CalorieSeries {
		if d.Value > 0 {
			return true
		}
	}
	return s.Calories7d > 0
}

// Load gathers the hub view. Real errors fail the page; missing optional
// slices are empty sections.
func (s *Service) Load(ctx context.Context, user users.User) (Snapshot, error) {
	var snap Snapshot

	loc := user.Location()
	now := time.Now().In(loc)
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(calorieWindow - 1))

	if s.activity != nil {
		total, err := s.activity.TotalCaloriesSince(ctx, user.ID, since)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Calories7d = total

		sessions, err := s.activity.List(ctx, user.ID, 100)
		if err != nil {
			return Snapshot{}, err
		}
		snap.CalorieSeries = buildCalorieSeries(loc, since, sessions)
	} else {
		snap.CalorieSeries = emptyCalorieSeries(loc)
	}

	if s.workouts != nil {
		stored, err := s.workouts.LatestPlan(ctx, user.ID)
		switch {
		case err == nil:
			snap.PlanID = stored.ID
			if day, ok := stored.Plan.NextSession(now); ok {
				snap.NextSession = &day
			}
		case !apperr.Is(err, apperr.ErrNotFound):
			return Snapshot{}, err
		}
	}

	if s.strava != nil {
		status, err := s.strava.Status(ctx, user.ID)
		if err != nil {
			status = strava.Status{Configured: s.strava.Configured()}
		}
		snap.StravaStatus = status
	}

	if s.meals != nil {
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		progress, err := s.meals.ForDay(ctx, user.ID, today)
		switch {
		case err == nil:
			snap.MealProgress = &progress
			snap.HasMealProgress = true
		case !apperr.Is(err, apperr.ErrNotFound):
			return Snapshot{}, err
		}
	}

	return snap, nil
}

func buildView(snap Snapshot) fitnesspages.Instruments {
	labels := make([]string, len(snap.CalorieSeries))
	values := make([]float64, len(snap.CalorieSeries))
	for i, d := range snap.CalorieSeries {
		labels[i] = d.Label
		values[i] = d.Value
	}

	return fitnesspages.Instruments{
		CalorieChart:    viz.Bar("fitness-calories", "Calories (kcal)", labels, values),
		HasCalories:     snap.HasCalorieChart(),
		Calories7d:      snap.Calories7d,
		PlanID:          snap.PlanID,
		NextSession:     snap.NextSession,
		HasMealProgress: snap.HasMealProgress,
		MealProgress:    snap.MealProgress,
	}
}

func buildCalorieSeries(loc *time.Location, since time.Time, sessions []activity.Session) []DayPoint {
	days := trailingDays(loc, calorieWindow)
	totals := make(map[string]float64, len(days))
	for _, d := range days {
		totals[d.Format("2006-01-02")] = 0
	}

	for _, session := range sessions {
		if session.Status != activity.StatusCompleted || session.EndedAt == nil || session.CaloriesBurned == nil {
			continue
		}
		ended := session.EndedAt.In(loc)
		if ended.Before(since) {
			continue
		}
		key := time.Date(ended.Year(), ended.Month(), ended.Day(), 0, 0, 0, 0, loc).Format("2006-01-02")
		if _, ok := totals[key]; ok {
			totals[key] += *session.CaloriesBurned
		}
	}

	out := make([]DayPoint, len(days))
	for i, d := range days {
		key := d.Format("2006-01-02")
		out[i] = DayPoint{Label: d.Format("Mon"), Value: totals[key]}
	}
	return out
}

func emptyCalorieSeries(loc *time.Location) []DayPoint {
	days := trailingDays(loc, calorieWindow)
	out := make([]DayPoint, len(days))
	for i, d := range days {
		out[i] = DayPoint{Label: d.Format("Mon"), Value: 0}
	}
	return out
}

func trailingDays(loc *time.Location, count int) []time.Time {
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	out := make([]time.Time, count)
	for i := range count {
		out[i] = today.AddDate(0, 0, -(count - 1 - i))
	}
	return out
}
