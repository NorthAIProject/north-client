package insights

import (
	"context"

	"golang.org/x/sync/errgroup"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// metric is one number this section can draw a page about.
//
// The registry is presentation, not storage: the slices hold far more than
// this, and the list decides which of it is worth a page of its own and what
// to call it. A metric missing from here is still logged and still charted on
// its domain page — it simply has no detail view. The health package's
// narrated-metric table works the same way, for the same reason.
type metric struct {
	Key   string
	Label string
	Unit  string

	// Decimals is how precise the number deserves to sound. Hours of sleep
	// earn one; millilitres and calories do not, and "1580.0ml" implies a
	// measurement nobody made.
	Decimals int

	// Mean makes a bucket wider than a day hold the average of its days
	// rather than their sum. True for anything measured per day — a week of
	// eight-hour nights is an eight-hour week, not a fifty-six hour one.
	// False for things that genuinely accumulate, like sessions or calories.
	Mean bool

	// Better is the direction that counts as improvement, for the trend
	// chip. Zero means neither: mood going up is good, and there is no good
	// direction for how many times somebody journalled.
	Better int

	// Href is the domain page this metric's detail links back up to.
	Href string

	load func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error)
}

func metrics() []metric {
	return []metric{
		{
			Key: "sleep", Decimals: 1, Label: "Sleep", Unit: "h", Mean: true, Better: 1,
			Href: "/app/insights/body",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				nights, err := s.sleep.ListBetween(ctx, user, rg)
				if err != nil {
					return nil, err
				}
				out := make([]point, 0, len(nights))
				for _, n := range nights {
					out = append(out, point{At: n.LocalDate, Value: float64(n.DurationMinutes) / 60})
				}
				return out, nil
			},
		},
		{
			Key: "water", Decimals: 0, Label: "Water", Unit: "ml", Mean: true, Better: 1,
			Href: "/app/insights/body",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				days, err := s.hydration.DaysBetween(ctx, user, rg)
				if err != nil {
					return nil, err
				}
				out := make([]point, 0, len(days))
				for _, d := range days {
					out = append(out, point{At: d.Date, Value: float64(d.TotalML)})
				}
				return out, nil
			},
		},
		{
			Key: "mood", Decimals: 1, Label: "Mood", Unit: "/5", Mean: true, Better: 1,
			Href: "/app/insights/mind",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				return checkInMetric(ctx, s, user, rg, func(mood, energy int) float64 { return float64(mood) })
			},
		},
		{
			Key: "energy", Decimals: 1, Label: "Energy", Unit: "/5", Mean: true, Better: 1,
			Href: "/app/insights/mind",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				return checkInMetric(ctx, s, user, rg, func(mood, energy int) float64 { return float64(energy) })
			},
		},
		{
			Key: "checkins", Label: "Check-ins", Better: 1,
			Href: "/app/insights/mind",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				return checkInMetric(ctx, s, user, rg, func(mood, energy int) float64 { return 1 })
			},
		},
		{
			Key: "journal", Label: "Journal entries", Better: 1,
			Href: "/app/insights/mind",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				entries, err := s.mind.ListBetween(ctx, user.ID, rg)
				if err != nil {
					return nil, err
				}
				out := make([]point, 0, len(entries))
				for _, e := range entries {
					out = append(out, point{At: e.CreatedAt, Value: 1})
				}
				return out, nil
			},
		},
		{
			Key: "burn", Decimals: 0, Label: "Calories burned", Unit: "kcal", Better: 1,
			Href: "/app/insights/training",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				return sessionMetric(ctx, s, user, rg, false)
			},
		},
		{
			Key: "sessions", Label: "Sessions", Better: 1,
			Href: "/app/insights/training",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				return sessionMetric(ctx, s, user, rg, true)
			},
		},
		{
			Key: "notes", Label: "Goal notes", Better: 1,
			Href: "/app/insights/progress",
			load: func(ctx context.Context, s *Service, user users.User, rg timerange.Range) ([]point, error) {
				notes, err := s.goals.UpdatesBetween(ctx, user.ID, rg)
				if err != nil {
					return nil, err
				}
				out := make([]point, 0, len(notes))
				for _, n := range notes {
					out = append(out, point{At: n.CreatedAt, Value: 1})
				}
				return out, nil
			},
		},
	}
}

func checkInMetric(ctx context.Context, s *Service, user users.User, rg timerange.Range, value func(mood, energy int) float64) ([]point, error) {
	rows, err := s.checkins.ListBetween(ctx, user.ID, rg)
	if err != nil {
		return nil, err
	}
	out := make([]point, 0, len(rows))
	for _, c := range rows {
		v := value(c.Mood, c.Energy)
		// A check-in filed without a rating is a missing measurement, not a
		// mood of zero, and averaging it in would drag the line down.
		if v <= 0 {
			continue
		}
		out = append(out, point{At: c.LocalDate, Value: v})
	}
	return out, nil
}

func sessionMetric(ctx context.Context, s *Service, user users.User, rg timerange.Range, count bool) ([]point, error) {
	sessions, err := s.activity.ListBetween(ctx, user.ID, rg)
	if err != nil {
		return nil, err
	}
	out := make([]point, 0, len(sessions))
	for _, sess := range sessions {
		if sess.EndedAt == nil {
			continue
		}
		v := 1.0
		if !count {
			if sess.CaloriesBurned == nil {
				continue
			}
			v = *sess.CaloriesBurned
		}
		out = append(out, point{At: *sess.EndedAt, Value: v})
	}
	return out, nil
}

// lookupMetric finds a metric by its URL key.
func lookupMetric(key string) (metric, bool) {
	for _, m := range metrics() {
		if m.Key == key {
			return m, true
		}
	}
	return metric{}, false
}

// MetricData is one metric over a window, beside the window before it.
type MetricData struct {
	Range  timerange.Range
	Metric metric

	Points []point
	Prior  []point
}

// Metric loads one metric for a window and for the window before it.
//
// The prior window is loaded every time because every claim this page makes —
// the trend chip, the comparison bars, the highlights — is a comparison, and a
// page that had to decide whether to fetch it would be a page that sometimes
// silently made no claim at all.
func (s *Service) Metric(ctx context.Context, user users.User, rg timerange.Range, key string) (MetricData, error) {
	m, ok := lookupMetric(key)
	if !ok {
		return MetricData{}, apperr.Wrap(apperr.ErrNotFound, "metric %q", key)
	}

	out := MetricData{Range: rg, Metric: m}
	prev := rg.Previous()

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		out.Points, err = m.load(gctx, s, user, rg)
		return
	})
	g.Go(func() (err error) {
		out.Prior, err = m.load(gctx, s, user, prev)
		return
	})

	if err := g.Wait(); err != nil {
		return MetricData{}, err
	}
	return out, nil
}
