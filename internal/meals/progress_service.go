package meals

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/calculator"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// MacroGoalLookup is the progress/recommendation services' view of the
// calculator: just enough to read the current macro plan, so this package
// need not depend on calculator.Service's concrete type. Satisfied directly
// by *calculator.Service.
type MacroGoalLookup interface {
	Current(ctx context.Context, userID uuid.UUID) (calculator.MacroPlan, error)
}

type TrackMealProgressService struct {
	foodLog *FoodLogService
	goals   MacroGoalLookup
}

func NewTrackMealProgressService(foodLog *FoodLogService, goals MacroGoalLookup) *TrackMealProgressService {
	return &TrackMealProgressService{foodLog: foodLog, goals: goals}
}

// Progress compares one day's logged intake against the current macro goal.
type Progress struct {
	Date time.Time

	Logged Macros
	Goal   Macros

	DeltaCalories float64
	DeltaProteinG float64
	DeltaFatG     float64
	DeltaCarbG    float64
}

// OverCalories reports whether the day's logged calories exceeded the goal.
func (p Progress) OverCalories() bool { return p.DeltaCalories > 0 }

// Summary renders a day's progress for the coach's context.
func (p Progress) Summary() string {
	direction := "under"
	delta := -p.DeltaCalories
	if p.OverCalories() {
		direction = "over"
		delta = p.DeltaCalories
	}
	return fmt.Sprintf(
		"%s: %.0f/%.0f kcal logged (%.0f kcal %s goal) — %.0fg protein / %.0fg fat / %.0fg carbs logged",
		p.Date.Format("2 Jan"), p.Logged.Calories, p.Goal.Calories, delta, direction,
		p.Logged.ProteinG, p.Logged.FatG, p.Logged.CarbG,
	)
}

// ForDay computes progress for a single date. apperr.ErrNotFound if the user
// has no macro goal generated yet.
func (s *TrackMealProgressService) ForDay(ctx context.Context, userID uuid.UUID, date time.Time) (Progress, error) {
	goal, err := s.goals.Current(ctx, userID)
	if err != nil {
		return Progress{}, err
	}

	logged, err := s.foodLog.DailyTotals(ctx, userID, date)
	if err != nil {
		return Progress{}, err
	}

	goalMacros := Macros{Calories: goal.CalorieGoal, ProteinG: goal.ProteinG, FatG: goal.FatG, CarbG: goal.CarbG}
	return Progress{
		Date: date, Logged: logged, Goal: goalMacros,
		DeltaCalories: logged.Calories - goalMacros.Calories,
		DeltaProteinG: logged.ProteinG - goalMacros.ProteinG,
		DeltaFatG:     logged.FatG - goalMacros.FatG,
		DeltaCarbG:    logged.CarbG - goalMacros.CarbG,
	}, nil
}

// ForRange computes progress for each day in [from, to], inclusive.
func (s *TrackMealProgressService) ForRange(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]Progress, error) {
	var out []Progress
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		p, err := s.ForDay(ctx, userID, d)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// adherenceTolerance is deliberately wider than
// GoalRecommendationService's driftThresholdKcal: adherence measures whether
// someone is logging consistently close to their goal at all, while the
// recommendation's drift check measures whether a consistent logger is
// consistently off-target. A narrower tolerance here would make consistent
// over/under-eating indistinguishable from not logging at all.
const adherenceTolerance = 0.20

// AdherenceScore is the fraction of the trailing `days` whose logged
// calories fell within +/-20% of the goal. apperr.ErrNotFound if there is no
// goal to measure against.
func (s *TrackMealProgressService) AdherenceScore(ctx context.Context, userID uuid.UUID, days int) (float64, error) {
	if days <= 0 {
		return 0, apperr.Wrap(apperr.ErrValidation, "days must be positive")
	}

	to := time.Now()
	from := to.AddDate(0, 0, -(days - 1))

	progress, err := s.ForRange(ctx, userID, from, to)
	if err != nil {
		return 0, err
	}

	within := 0
	for _, p := range progress {
		if p.Goal.Calories <= 0 {
			continue
		}
		tolerance := p.Goal.Calories * adherenceTolerance
		if p.DeltaCalories >= -tolerance && p.DeltaCalories <= tolerance {
			within++
		}
	}

	return float64(within) / float64(len(progress)), nil
}
