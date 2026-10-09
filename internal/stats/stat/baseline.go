package stat

import (
	"math"
	"time"
)

// BaselineWindow is how many days before a day make up its baseline: four
// weeks, long enough that one odd week does not move it, short enough to
// follow someone who is getting fitter.
const BaselineWindow = 28

// BaselineMinDays is the fewest days with a value before a baseline is
// claimed. Fewer than a week says more about the week than the person.
const BaselineMinDays = 7

// Where a value sits against a baseline.
const (
	Above = "above"
	Usual = "usual"
	Below = "below"
)

// Baseline is what a measure usually reads for one person: the mean and
// spread of their daily values over the BaselineWindow days before a day.
type Baseline struct {
	Mean float64
	SD   float64 // sample standard deviation
	Days int
}

// BaselineBefore summarises the daily values in the BaselineWindow days
// before day. Day itself is left out, so a day is never compared with
// itself. values hold one entry per day, dated at the start of the day.
func BaselineBefore(values []DayValue, day time.Time) (Baseline, bool) {
	since := day.AddDate(0, 0, -BaselineWindow)
	var in []float64
	for _, v := range values {
		if !v.Day.Before(since) && v.Day.Before(day) {
			in = append(in, v.Value)
		}
	}
	if len(in) < BaselineMinDays {
		return Baseline{}, false
	}

	m := mean(in)
	var squares float64
	for _, v := range in {
		squares += (v - m) * (v - m)
	}
	return Baseline{Mean: m, SD: math.Sqrt(squares / float64(len(in)-1)), Days: len(in)}, true
}

// Place says how many standard deviations v sits from the baseline, and
// whether that is above, within or below the usual range. Within one
// standard deviation is usual: about two days in three land there. A
// baseline with no spread calls everything usual, because a person whose
// readings never moved has given no measure of what counts as a big move.
func (b Baseline) Place(v float64) (z float64, state string) {
	if b.SD == 0 {
		return 0, Usual
	}
	z = (v - b.Mean) / b.SD
	switch {
	case z >= 1:
		return z, Above
	case z <= -1:
		return z, Below
	default:
		return z, Usual
	}
}
