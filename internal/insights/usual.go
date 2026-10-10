package insights

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Usual is a health metric's latest day placed against the weeks before it.
type Usual struct {
	Latest   point
	Baseline stat.Baseline
	Z        float64
	State    string // stat.Above, stat.Usual or stat.Below
}

// usualOf places the last of a metric's daily points against the
// stat.BaselineWindow days before it. points are one per day, oldest first.
func usualOf(points []point) (Usual, bool) {
	if len(points) == 0 {
		return Usual{}, false
	}
	days := make([]stat.DayValue, len(points))
	for i, p := range points {
		days[i] = stat.DayValue{Day: p.At, Value: p.Value}
	}
	latest := points[len(points)-1]
	b, ok := stat.BaselineBefore(days, latest.At)
	if !ok {
		return Usual{}, false
	}
	z, state := b.Place(latest.Value)
	return Usual{Latest: latest, Baseline: b, Z: z, State: state}, true
}

// usualWindow is the range a baseline for a latest day on at needs: the
// baseline's weeks and the day itself.
func usualWindow(at time.Time) timerange.Range {
	return timerange.Between(at.AddDate(0, 0, -stat.BaselineWindow), at.AddDate(0, 0, 1))
}

// healthRecentDays is how far back the health list looks. A metric with no
// reading in that time is not something the person is tracking any more.
const healthRecentDays = 14

// HealthRow is one health metric in the list: its last fortnight and where
// its latest day sits against the person's usual.
type HealthRow struct {
	Metric metric
	// Recent is one point per day with a value, oldest first; the last is
	// the latest day.
	Recent []point
	Usual  *Usual
}

// Health lists the health metrics this person has recent readings for, in
// catalog order. A metric with nothing in the last healthRecentDays days is
// left out rather than shown empty, so the list is what they actually track.
func (s *Service) Health(ctx context.Context, user users.User, now time.Time) ([]HealthRow, error) {
	today := timerange.StartOfDay(now.In(user.Location()))
	window := timerange.Between(today.AddDate(0, 0, -healthRecentDays-stat.BaselineWindow), today.AddDate(0, 0, 1))
	recentSince := today.AddDate(0, 0, -healthRecentDays+1)

	var catalog []metric
	for _, m := range metrics() {
		if m.Health {
			catalog = append(catalog, m)
		}
	}
	loaded := make([][]point, len(catalog))
	g, gctx := errgroup.WithContext(ctx)
	for i, m := range catalog {
		g.Go(func() (err error) {
			loaded[i], err = m.load(gctx, s, user, window)
			return
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var out []HealthRow
	for i, m := range catalog {
		points := loaded[i]
		var recent []point
		for _, p := range points {
			if !p.At.Before(recentSince) {
				recent = append(recent, p)
			}
		}
		if len(recent) == 0 {
			continue
		}
		row := HealthRow{Metric: m, Recent: recent}
		if u, ok := usualOf(points); ok {
			row.Usual = &u
		}
		out = append(out, row)
	}
	return out, nil
}
