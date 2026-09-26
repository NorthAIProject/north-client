package day

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Trend windows, in days: long enough to see a direction, short enough that
// the direction is about now.
const (
	weightWindow   = 84 // twelve weeks
	pressureWindow = 90
	dailyWindow    = 14
	fastsWindow    = 30
	recentFasts    = 8
)

// Trends is the Overview's row of trend cards.
type Trends struct {
	Weight       day.Series
	Systolic     day.Series
	ActiveEnergy day.Series
	Sleep        day.Series
	Caffeine     day.Series
	Fasts        []day.FastBar
}

// Trends gathers the trend cards. Like Load, a slice that is not wired is an
// empty card, and a real error fails the whole row.
func (s *Service) Trends(ctx context.Context, user users.User) (Trends, error) {
	loc := user.Location()
	now := s.now().In(loc)
	since := func(days int) time.Time { return timerange.StartOfDay(now).AddDate(0, 0, -days+1) }
	until := now.Add(time.Minute)

	var out Trends
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var points []day.Point
		if s.biometrics != nil {
			history, err := s.biometrics.History(gctx, user.ID, 100)
			if err != nil {
				return err
			}
			for _, b := range history {
				if !b.CreatedAt.Before(since(weightWindow)) {
					points = append(points, day.Point{At: b.CreatedAt, Value: b.WeightKg})
				}
			}
		}
		if s.health != nil {
			rows, err := s.health.Between(gctx, user.ID, metricBodyMass, since(weightWindow), until)
			if err != nil {
				return err
			}
			for _, r := range rows {
				points = append(points, day.Point{At: r.StartedAt, Value: r.Value})
			}
		}
		out.Weight = day.Measurements("weight", "kg", weightWindow, points)
		return nil
	})

	if s.health != nil {
		g.Go(func() error {
			rows, err := s.health.Between(gctx, user.ID, metricSystolic, since(pressureWindow), until)
			if err != nil {
				return err
			}
			points := make([]day.Point, len(rows))
			for i, r := range rows {
				points[i] = day.Point{At: r.StartedAt, Value: r.Value}
			}
			out.Systolic = day.Measurements("systolic", "mmHg", pressureWindow, points)
			return nil
		})
		g.Go(func() error {
			rows, err := s.health.Between(gctx, user.ID, metricActiveCalories, since(dailyWindow), until)
			if err != nil {
				return err
			}
			points := make([]day.Point, len(rows))
			for i, r := range rows {
				points[i] = day.Point{At: r.StartedAt, Value: r.Value}
			}
			out.ActiveEnergy = day.DailyTotals("active_energy", "kcal", dailyWindow, points, loc)
			return nil
		})
	}

	if s.sleep != nil {
		g.Go(func() error {
			logs, err := s.sleep.ListBetween(gctx, user, timerange.Between(since(dailyWindow), until))
			if err != nil {
				return err
			}
			points := make([]day.Point, len(logs))
			for i, l := range logs {
				points[i] = day.Point{At: l.LocalDate, Value: float64(l.DurationMinutes) / 60}
			}
			out.Sleep = day.DailyTotals("sleep", "h", dailyWindow, points, loc)
			return nil
		})
	}

	if s.caffeine != nil {
		g.Go(func() error {
			entries, err := s.caffeine.Between(gctx, user, timerange.Between(since(dailyWindow), until))
			if err != nil {
				return err
			}
			points := make([]day.Point, len(entries))
			for i, e := range entries {
				points[i] = day.Point{At: e.LoggedAt, Value: float64(e.MG)}
			}
			out.Caffeine = day.DailyTotals("caffeine", "mg", dailyWindow, points, loc)
			return nil
		})
	}

	if s.fasting != nil {
		g.Go(func() error {
			fasts, err := s.fasting.Overlapping(gctx, user, timerange.Between(since(fastsWindow), until))
			if err != nil {
				return err
			}
			for _, f := range fasts {
				if f.Open() {
					continue
				}
				out.Fasts = append(out.Fasts, day.FastBar{StartedAt: f.StartedAt, Hours: f.Elapsed(now).Hours(), TargetHours: f.TargetHours})
				if len(out.Fasts) == recentFasts {
					break
				}
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return Trends{}, err
	}
	return out, nil
}
