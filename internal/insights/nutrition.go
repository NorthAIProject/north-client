package insights

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// NutritionDay is one day's logged intake, rolled up from its entries.
type NutritionDay struct {
	Date   time.Time
	Macros meals.Macros
}

// NutritionData is what was eaten over a window, and the plan it is measured
// against.
type NutritionData struct {
	Range timerange.Range

	Days    []NutritionDay
	Entries int

	Goal calculator.MacroPlan

	// HasGoal is false for somebody who has not run the calculator. Their
	// intake is still charted; it simply is not judged, because there is no
	// target to judge it against.
	HasGoal bool
}

// Nutrition loads the window's food logs and the current macro plan.
func (s *Service) Nutrition(ctx context.Context, user users.User, rg timerange.Range) (NutritionData, error) {
	out := NutritionData{Range: rg}
	if s.food == nil {
		return out, nil
	}

	var entries []meals.FoodLogEntry

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		entries, err = s.food.Range(gctx, user.ID, rg.Since, rg.Until)
		return
	})
	g.Go(func() error {
		if s.macroGoals == nil {
			return nil
		}
		plan, err := s.macroGoals.Current(gctx, user.ID)
		if apperr.Is(err, apperr.ErrNotFound) {
			// Not having run the calculator is a normal state, not a failure.
			return nil
		}
		if err != nil {
			return err
		}
		out.Goal, out.HasGoal = plan, true
		return nil
	})

	if err := g.Wait(); err != nil {
		return NutritionData{}, err
	}

	out.Days = rollUpByDay(entries, rg.Location())
	out.Entries = len(entries)
	return out, nil
}

// rollUpByDay turns individual food logs into one row per day.
//
// Per day rather than per entry because that is the grain every question here
// asks — "did they hit their calories" is a question about a day, not about a
// snack — and because the score and the chart both need days.
func rollUpByDay(entries []meals.FoodLogEntry, loc *time.Location) []NutritionDay {
	byDay := make(map[time.Time]meals.Macros, len(entries))
	var order []time.Time

	for _, e := range entries {
		day := timerange.StartOfDay(e.LogDate.In(loc))
		if _, seen := byDay[day]; !seen {
			order = append(order, day)
		}
		byDay[day] = byDay[day].Add(e.Macros)
	}

	out := make([]NutritionDay, 0, len(order))
	for _, day := range order {
		out = append(out, NutritionDay{Date: day, Macros: byDay[day]})
	}
	return out
}
