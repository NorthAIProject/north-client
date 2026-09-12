package insights

import (
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// point is one dated value on its way into a chart series.
type point struct {
	At    time.Time
	Value float64
}

// bucketed places dated values into the range's buckets, summing points that
// share one.
//
// It exists because every builder here was doing this by hand, and doing it
// the same wrong way: matching a bucket by formatting its start as
// "2006-01-02". That holds only at day grain. At week grain a bucket starts
// every seventh day, so six days in seven matched nothing and vanished from
// the chart; at hour grain all twenty-four buckets formatted to the same date,
// so a day's total was drawn in every hour of it. Range.Index already answered
// this correctly and had no callers.
func bucketed(rg timerange.Range, points []point) []float64 {
	buckets := rg.Buckets()
	out := make([]float64, len(buckets))

	for _, p := range points {
		if i := rg.Index(buckets, p.At); i >= 0 {
			out[i] += p.Value
		}
	}
	return out
}

// bucketLabels is the buckets' captions, in order.
func bucketLabels(rg timerange.Range) []string {
	buckets := rg.Buckets()
	out := make([]string, len(buckets))
	for i, b := range buckets {
		out[i] = b.Label
	}
	return out
}

// bucketedMean averages the readings in each bucket instead of summing them.
//
// Per-day measurements need this: a week bucket holding seven eight-hour
// nights is an eight-hour average, not a fifty-six hour one, and an axis whose
// meaning changed with the selected range would make the chart unreadable.
// Buckets with no reading are zero rather than divided by none, and a day
// nobody logged is left out of the average rather than counted as a zero.
func bucketedMean(rg timerange.Range, points []point) []float64 {
	buckets := rg.Buckets()
	sums := make([]float64, len(buckets))
	counts := make([]int, len(buckets))

	for _, p := range points {
		if i := rg.Index(buckets, p.At); i >= 0 {
			sums[i] += p.Value
			counts[i]++
		}
	}

	for i, n := range counts {
		if n > 0 {
			sums[i] /= float64(n)
		}
	}
	return sums
}

// periodNoun is the singular name of one bucket at this range's grain.
//
// The highlight rules need it to say "unlogged for 10 months" rather than
// "unlogged for 10", which leaves the reader to guess ten of what.
func periodNoun(rg timerange.Range) string {
	switch rg.Grain {
	case timerange.GrainHour:
		return "hour"
	case timerange.GrainWeek:
		return "week"
	case timerange.GrainMonth:
		return "month"
	default:
		return "day"
	}
}
