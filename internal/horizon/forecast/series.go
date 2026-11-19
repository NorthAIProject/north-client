package forecast

import (
	"math"
	"time"
)

// bandHorizons are the days ahead at which past forecasts are replayed. A
// band for any other day is read off the line between its neighbours.
var bandHorizons = []int{7, 14, 28, 56, 84}

// recordHorizonDays is the horizon the track record is stated for: far
// enough ahead to mean something, near enough that most people have a
// history of it.
const recordHorizonDays = 28

// AheadPoint is the range for one day ahead.
type AheadPoint struct {
	OffsetDays int
	Range
}

// Series is one forecast: where a number is now and the range it is expected
// to be in on each day ahead.
type Series struct {
	Status string
	// Needs says what is missing when Status is insufficient.
	Needs []Need

	// Now is the fitted level today, not the last raw observation.
	Now   float64
	Trend Trend
	// Points are the days ahead, from zero. They stop where the history
	// stops being long enough to justify them, so they may end before
	// MaxOffset.
	Points []AheadPoint

	Track TrackRecord
	// FromHistory is true when the ranges come from this person's own past
	// misses, false when they fall back to the series' scatter.
	FromHistory bool
}

// At is the range the given days ahead, or false when the series does not
// reach that far.
func (s Series) At(offsetDays int) (Range, bool) {
	for _, p := range s.Points {
		if p.OffsetDays == offsetDays {
			return p.Range, true
		}
	}
	return Range{}, false
}

// Last is the furthest point ahead, or false for a series with none.
func (s Series) Last() (AheadPoint, bool) {
	if len(s.Points) == 0 {
		return AheadPoint{}, false
	}
	return s.Points[len(s.Points)-1], true
}

// insufficient is a series that cannot be forecast yet.
func insufficient(needs ...Need) Series {
	return Series{Status: StatusInsufficient, Needs: needs}
}

// adjust decides a day's predicted value when something the trend does not
// know about is in play, such as weeks off in a what-if. ahead gives the
// plain trend any number of days from now. Nil means the plain trend.
type adjust func(offsetDays int, ahead func(days float64) float64) float64

// build draws the series: one fit on everything known, a band per horizon
// from replaying that fit on the past, and a point per offset.
//
// It never draws further ahead than the history is long. Twelve weeks
// forecast from three weeks of data is a guess with a chart.
func build(points []Point, now time.Time, fit Fitter, change adjust) (Series, bool) {
	pts := until(sorted(points), now)
	trend, ok := fit(pts)
	if !ok {
		return Series{}, false
	}

	bands := make([]Band, len(bandHorizons))
	fromHistory := false
	var record TrackRecord
	for i, h := range bandHorizons {
		misses := Misses(pts, now, h, fit)
		band, ok := BandFrom(misses)
		if !ok {
			band = NoiseBand(trend.Spread, h)
		}
		bands[i] = band
		if h == recordHorizonDays {
			record = Score(misses, h/7)
			fromHistory = ok
		}
	}
	// A longer horizon is never more certain than a shorter one. Small
	// samples can say otherwise by luck; this stops the fan pinching.
	for i := 1; i < len(bands); i++ {
		bands[i].Low = math.Min(bands[i].Low, bands[i-1].Low)
		bands[i].High = math.Max(bands[i].High, bands[i-1].High)
	}

	span := int(days(now.Sub(pts[0].At)))
	stale := days(now.Sub(trend.At))
	series := Series{
		Status:      StatusReady,
		Trend:       trend,
		Now:         trend.Ahead(stale),
		Track:       record,
		FromHistory: fromHistory,
	}
	for _, offset := range Offsets() {
		if offset > span {
			break
		}
		ahead := func(d float64) float64 { return trend.Ahead(stale + d) }
		mid := ahead(float64(offset))
		if change != nil {
			mid = change(offset, ahead)
		}
		series.Points = append(series.Points, AheadPoint{
			OffsetDays: offset,
			Range:      bandAt(bands, offset).Around(mid),
		})
	}
	return series, true
}

// bandAt reads the band for any day off the horizons it was measured at:
// nothing at day zero, a straight line between measured horizons.
func bandAt(bands []Band, offsetDays int) Band {
	if offsetDays <= 0 {
		return Band{}
	}
	prevDay, prev := 0, Band{}
	for i, h := range bandHorizons {
		if offsetDays <= h {
			t := float64(offsetDays-prevDay) / float64(h-prevDay)
			return Band{
				Low:  prev.Low + t*(bands[i].Low-prev.Low),
				High: prev.High + t*(bands[i].High-prev.High),
			}
		}
		prevDay, prev = h, bands[i]
	}
	return prev
}
