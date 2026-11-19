package forecast

import (
	"math"
	"time"
)

// Backtest settings.
const (
	// backtestStep is how far apart the past forecasts are made.
	backtestStep = 7 * Day
	// backtestLookback is how far back they go. Older training says little
	// about how predictable this person is now.
	backtestLookback = 26 * 7 * Day
	// actualWindow is how close to the target day an observation must be to
	// count as what actually happened. People do not train a lift, or weigh
	// in, on the exact day a forecast names.
	actualWindow = 4 * Day
	// minErrors is how many past misses a range needs before it is built
	// from them. Below it the range comes from the series' own noise.
	minErrors = 5
	// MinScored is how many past forecasts must have been scored before a
	// track record is shown. "Right 2 of 2 times" is not a record.
	MinScored = 5
	// rangeLow and rangeHigh are the quantiles of past misses a range spans:
	// four in five outcomes, if the future is as predictable as the past.
	rangeLow  = 0.10
	rangeHigh = 0.90
	// minWidth is the narrowest a range is ever drawn, each side, as a
	// fraction. A handful of lucky past forecasts must not produce a range
	// that claims certainty.
	minWidth = 0.01
)

// Fitter makes a forecast from the points known at one moment. It returns
// false when it would not forecast from that little. Backtest calls it with
// only the points up to each past origin, so a fitter must never look at
// anything it was not handed.
type Fitter func(known []Point) (Trend, bool)

// Miss is one past forecast scored against what happened.
type Miss struct {
	Origin    time.Time
	Predicted float64
	Actual    float64
	// Spread is the scatter of the fit made at the origin: what the range
	// was drawn from before there were enough past misses to draw it from.
	Spread float64
}

// Relative is the miss as a fraction of the prediction: +0.03 means the
// person landed 3 % above it.
func (m Miss) Relative() float64 {
	if m.Predicted == 0 {
		return 0
	}
	return m.Actual/m.Predicted - 1
}

// Misses replays the forecast at every past week: fit on what was known
// then, predict horizonDays ahead, and compare with what was then observed.
// Oldest first. Origins whose target day has no observation near it are
// skipped, as are origins the fitter declines.
func Misses(points []Point, now time.Time, horizonDays int, fit Fitter) []Miss {
	pts := sorted(points)
	if len(pts) == 0 {
		return nil
	}
	horizon := time.Duration(horizonDays) * Day
	earliest := now.Add(-backtestLookback)
	if first := pts[0].At; first.After(earliest) {
		earliest = first
	}

	var out []Miss
	// Walk back from the newest origin whose target is not in the future,
	// then reverse, so origins are the same whatever day the history began.
	for origin := now.Add(-horizon); !origin.Before(earliest); origin = origin.Add(-backtestStep) {
		trend, ok := fit(until(pts, origin))
		if !ok {
			continue
		}
		target := origin.Add(horizon)
		actual, ok := observedNear(pts, target)
		if !ok {
			continue
		}
		// The trend's own clock starts at its last point, which may be some
		// days before the origin.
		ahead := days(target.Sub(trend.At))
		out = append(out, Miss{Origin: origin, Predicted: trend.Ahead(ahead), Actual: actual, Spread: trend.Spread})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// ObservedNear is what a series actually was around t, for scoring a stored
// forecast. Points may be in any order.
func ObservedNear(points []Point, t time.Time) (float64, bool) {
	return observedNear(sorted(points), t)
}

// observedNear is what the series actually was around t: the median of the
// observations within actualWindow of it.
func observedNear(sortedPoints []Point, t time.Time) (float64, bool) {
	near := until(since(sortedPoints, t.Add(-actualWindow)), t.Add(actualWindow))
	if len(near) == 0 {
		return 0, false
	}
	values := make([]float64, len(near))
	for i, p := range near {
		values[i] = p.Value
	}
	return median(values), true
}

// Band is how far below and above a prediction the outcome may land, as
// fractions of it: Low is zero or negative, High zero or positive.
type Band struct {
	Low  float64
	High float64
	// FromHistory is true when the band was built from this person's past
	// misses, false when it fell back to the series' noise.
	FromHistory bool
}

// Around applies the band to a prediction.
func (b Band) Around(mid float64) Range {
	low, high := mid*(1+b.Low), mid*(1+b.High)
	if low > high { // a negative mid flips them
		low, high = high, low
	}
	return Range{Low: low, Mid: mid, High: high}
}

// BandFrom builds the band for one horizon from past misses at that horizon.
// It always includes the prediction itself, and is never narrower than
// minWidth a side.
func BandFrom(misses []Miss) (Band, bool) {
	if len(misses) < minErrors {
		return Band{}, false
	}
	rel := make([]float64, len(misses))
	for i, m := range misses {
		rel[i] = m.Relative()
	}
	return Band{
		Low:         math.Min(quantile(rel, rangeLow), -minWidth),
		High:        math.Max(quantile(rel, rangeHigh), minWidth),
		FromHistory: true,
	}, true
}

// NoiseBand is the fallback when there are too few past misses: the series'
// own scatter, widening with the square root of the horizon as an
// accumulating error does. Deliberately generous (two standard deviations at
// four weeks), because it has not been checked against anything.
func NoiseBand(spread float64, horizonDays int) Band {
	width := math.Max(spread, minWidth) * 2 * math.Sqrt(math.Max(float64(horizonDays), 1)/28)
	width = math.Max(width, minWidth)
	return Band{Low: -width, High: width}
}

// TrackRecord is how often past forecasts at one horizon landed inside the
// range they would have been given.
type TrackRecord struct {
	HorizonWeeks int
	Hits         int
	Total        int
}

// Shown reports whether there are enough scored forecasts to state a record.
func (t TrackRecord) Shown() bool { return t.Total >= MinScored }

// Score counts how many past forecasts landed in the range they would have
// been shown with at the time. Each is judged out of sample: against a band
// built only from the misses before it, or, while there were too few of
// those, against the scatter of the fit made that day, which is the range
// the person would then have seen. Judging a forecast against a band that
// already contains its own miss would flatter the record.
//
// It is slightly strict: a drawn range is also never narrower than the one
// for a shorter horizon, which can only have made the real one wider.
func Score(misses []Miss, horizonWeeks int) TrackRecord {
	record := TrackRecord{HorizonWeeks: horizonWeeks}
	for i, m := range misses {
		band, ok := BandFrom(misses[:i])
		if !ok {
			band = NoiseBand(m.Spread, horizonWeeks*7)
		}
		record.Total++
		if band.Around(m.Predicted).Contains(m.Actual) {
			record.Hits++
		}
	}
	return record
}
