package forecast

import (
	"math"
	"time"
)

// DampingDays is how quickly a trend is assumed to flatten. Nobody adds
// 2.5 kg a week for a year: progress slows, and a straight line through the
// last few weeks overshoots badly by week twelve. With this time constant a
// trend delivers about two thirds of its straight-line gain by eight weeks
// and never more than eight weeks' worth in total.
const DampingDays = 56.0

// Trend is a series reduced to where it is now and how fast it is moving.
type Trend struct {
	// At is the moment Level describes: the last observation.
	At time.Time
	// Level is the fitted value at At, not the last raw point, so one
	// unusually good or bad day does not move the whole forecast.
	Level float64
	// SlopePerDay is the change per day, in the series' own unit.
	SlopePerDay float64
	// Spread is the typical size of a point's miss from the fitted line, as
	// a fraction of Level: the series' own noise.
	Spread float64
	// N is how many points the fit used.
	N int
}

// Fit reduces points to a Trend by the Theil–Sen estimator: the slope is the
// median of the slopes between every pair of points, and the level is the
// median of what each point implies for the last day.
//
// Chosen over least squares because training data has outliers that are not
// noise to average over: a missed lift, a set logged with the wrong plate. A
// median ignores up to about a quarter of the points being wild. It needs two
// points on different days.
func Fit(points []Point) (Trend, bool) {
	pts := sorted(points)
	if len(pts) < 2 {
		return Trend{}, false
	}
	last := pts[len(pts)-1].At

	slopes := make([]float64, 0, len(pts)*(len(pts)-1)/2)
	for i := 0; i < len(pts); i++ {
		for j := i + 1; j < len(pts); j++ {
			gap := days(pts[j].At.Sub(pts[i].At))
			if gap <= 0 {
				continue
			}
			slopes = append(slopes, (pts[j].Value-pts[i].Value)/gap)
		}
	}
	if len(slopes) == 0 {
		return Trend{}, false
	}
	slope := median(slopes)

	levels := make([]float64, len(pts))
	for i, p := range pts {
		levels[i] = p.Value + slope*days(last.Sub(p.At))
	}
	level := median(levels)

	misses := make([]float64, len(pts))
	for i := range levels {
		misses[i] = math.Abs(levels[i] - level)
	}
	spread := 0.0
	if level != 0 {
		// 1.4826 scales a median absolute deviation to a standard deviation
		// for normally distributed noise.
		spread = 1.4826 * median(misses) / math.Abs(level)
	}

	return Trend{At: last, Level: level, SlopePerDay: slope, Spread: spread, N: len(pts)}, true
}

// Ahead is the trend's value the given days after At, with the slope
// flattening over DampingDays. Days before At read the line backwards,
// undamped.
func (t Trend) Ahead(daysAhead float64) float64 {
	if daysAhead <= 0 {
		return t.Level + t.SlopePerDay*daysAhead
	}
	return t.Level + t.SlopePerDay*DampingDays*(1-math.Exp(-daysAhead/DampingDays))
}

// PercentPerWeek is the slope as a percentage of the level per week.
func (t Trend) PercentPerWeek() float64 {
	if t.Level == 0 {
		return 0
	}
	return 100 * 7 * t.SlopePerDay / t.Level
}
