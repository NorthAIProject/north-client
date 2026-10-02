package insights

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Area weeks: how far back the scoreboard looks. Eight weeks shows a trend
// without the first weeks of an account dragging it.
const (
	AreaWeeksDefault = 8
	AreaWeeksMax     = 12

	// areaParallel bounds how many weeks load at once. Each week is a full
	// summary, itself a fan-out, so an unbounded loop is a burst of queries.
	areaParallel = 3
)

// AreaPoint is one area's score for one week.
type AreaPoint struct {
	Points  int
	HasData bool
}

// Area is one life area's score now and its weekly trend, oldest first.
type Area struct {
	Key, Label string
	Now        score.Score
	Trend      []AreaPoint
}

// Areas is the life-area scoreboard: the same areas the summary scores,
// week by week. Weeks are the Mondays of each week, oldest first; the last is
// the week in progress.
type Areas struct {
	Weeks []time.Time
	Areas []Area
}

// AreaTrend scores each area for each of the last weeks, Monday to Sunday in
// the person's time zone, the current week so far last.
func (s *Service) AreaTrend(ctx context.Context, user users.User, weeks int, now time.Time) (Areas, error) {
	windows := areaWindows(now.In(user.Location()), weeks)
	weeks = len(windows)
	mondays := make([]time.Time, weeks)
	for i, w := range windows {
		mondays[i] = w.Since
	}

	scores := make([][]score.Score, weeks)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(areaParallel)
	for i, w := range windows {
		g.Go(func() error {
			data, err := s.Summary(gctx, user, w)
			if err != nil {
				return err
			}
			scores[i] = summaryScores(data)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Areas{}, err
	}

	out := Areas{Weeks: mondays, Areas: make([]Area, 0, len(domains))}
	// summaryScores returns one score per entry in domains, in the same
	// order; buildSummaryView relies on that too.
	for d, dom := range domains {
		a := Area{Key: dom.Key, Label: dom.Label, Trend: make([]AreaPoint, 0, weeks)}
		for w := range mondays {
			sc := scores[w][d]
			a.Trend = append(a.Trend, AreaPoint{Points: sc.Points, HasData: sc.HasData})
		}
		a.Now = scores[weeks-1][d]
		out.Areas = append(out.Areas, a)
	}
	return out, nil
}

// areaWindows are the weeks the scoreboard scores, oldest first: whole
// Monday-to-Monday weeks, then the current week up to now. weeks outside
// 2..AreaWeeksMax means the default.
func areaWindows(now time.Time, weeks int) []timerange.Range {
	if weeks < 2 || weeks > AreaWeeksMax {
		weeks = AreaWeeksDefault
	}
	thisMonday := timerange.StartOfWeek(now)
	out := make([]timerange.Range, weeks)
	for i := range out {
		monday := thisMonday.AddDate(0, 0, -7*(weeks-1-i))
		until := monday.AddDate(0, 0, 7)
		if i == weeks-1 {
			until = now
		}
		out[i] = timerange.Between(monday, until)
	}
	return out
}
