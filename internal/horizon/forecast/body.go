package forecast

import "time"

// Body thresholds.
const (
	// weightWindow is how far back the weight trend looks. Six weeks spans
	// a couple of water-weight swings without reaching into a previous diet.
	weightWindow = 42 * Day
	// WeightMinDays is how many separate days of weigh-ins it takes.
	WeightMinDays = 5
	weightMinSpan = 14 * Day

	// waistWindow is longer: people measure a waist every week or two.
	waistWindow = 12 * 7 * Day
	// WaistMinChecks is how many measurements it takes.
	WaistMinChecks = 3
	waistMinSpan   = 21 * Day
)

// DailyMeans reduces weigh-ins to one point per calendar day, the mean of
// that day's, so three readings in one morning do not count three times.
// The day is read in each point's own location.
func DailyMeans(points []Point) []Point {
	type acc struct {
		sum float64
		n   int
		at  time.Time
	}
	byDay := map[time.Time]*acc{}
	for _, p := range points {
		y, m, d := p.At.Date()
		day := time.Date(y, m, d, 0, 0, 0, 0, p.At.Location())
		a := byDay[day]
		if a == nil {
			a = &acc{at: p.At}
			byDay[day] = a
		}
		a.sum += p.Value
		a.n++
		if p.At.After(a.at) {
			a.at = p.At
		}
	}
	out := make([]Point, 0, len(byDay))
	for _, a := range byDay {
		out = append(out, Point{At: a.at, Value: a.sum / float64(a.n)})
	}
	return sorted(out)
}

// windowFit is a fitter over the last window of a series, requiring a
// minimum number of points across a minimum span.
func windowFit(window time.Duration, minPoints int, minSpan time.Duration) Fitter {
	return func(known []Point) (Trend, bool) {
		if len(known) == 0 {
			return Trend{}, false
		}
		recent := since(known, known[len(known)-1].At.Add(-window))
		if len(recent) < minPoints || recent[len(recent)-1].At.Sub(recent[0].At) < minSpan {
			return Trend{}, false
		}
		return Fit(recent)
	}
}

// ForecastWeight forecasts body weight from weigh-ins (any number per day).
func ForecastWeight(weighIns []Point, now time.Time) Series {
	pts := until(DailyMeans(weighIns), now)
	recent := since(pts, now.Add(-weightWindow))
	series, ok := build(pts, now, windowFit(weightWindow, WeightMinDays, weightMinSpan), nil)
	// The trend must also be about now: a fit that only succeeds on an old
	// stretch of weigh-ins is not a forecast of today's weight.
	if !ok || len(recent) < WeightMinDays {
		return insufficient(Need{Code: NeedWeighIns, Have: len(recent), Want: WeightMinDays})
	}
	return series
}

// ForecastWaist forecasts a waist measurement. It is never inferred from
// weight: two people losing the same kilos lose them in different places.
func ForecastWaist(checks []Point, now time.Time) Series {
	pts := until(sorted(checks), now)
	recent := since(pts, now.Add(-waistWindow))
	series, ok := build(pts, now, windowFit(waistWindow, WaistMinChecks, waistMinSpan), nil)
	if !ok || len(recent) < WaistMinChecks {
		return insufficient(Need{Code: NeedWaistChecks, Have: len(recent), Want: WaistMinChecks})
	}
	return series
}

// ETA is when a series is expected to reach a target: a window of days
// ahead, earliest to latest.
type ETA struct {
	// Reachable is false when the trend is not heading for the target, or
	// does not get there inside the forecast.
	Reachable bool
	// EarliestDays and LatestDays bound the arrival. LatestDays is zero and
	// Open is true when only the favourable edge of the range gets there.
	EarliestDays int
	LatestDays   int
	Open         bool
	// Reached is true when the fitted level is already at the target.
	Reached bool
}

// ETAFor reads when the series crosses target. The earliest day is when the
// favourable edge of the range reaches it; the latest is when the
// unfavourable edge does. Falling toward a lower target or rising toward a
// higher one are both handled.
func ETAFor(s Series, target float64) ETA {
	if s.Status != StatusReady || target <= 0 || len(s.Points) == 0 {
		return ETA{}
	}
	down := target < s.Now
	reached := func(v float64) bool {
		if down {
			return v <= target
		}
		return v >= target
	}
	if reached(s.Now) || s.Now == target {
		return ETA{Reachable: true, Reached: true}
	}

	eta := ETA{}
	for _, p := range s.Points {
		favourable, unfavourable := p.High, p.Low
		if down {
			favourable, unfavourable = p.Low, p.High
		}
		if !eta.Reachable && reached(favourable) {
			eta.Reachable = true
			eta.EarliestDays = p.OffsetDays
			eta.Open = true
		}
		if eta.Reachable && reached(unfavourable) {
			eta.LatestDays = p.OffsetDays
			eta.Open = false
			break
		}
	}
	return eta
}
