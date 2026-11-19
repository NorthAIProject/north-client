// Package forecast turns a person's own history into ranges for where their
// training is heading.
//
// It is arithmetic, not a model call: every number here can be worked out by
// hand from the logged sets and weigh-ins, and the same input always gives the
// same output. Three rules shape all of it.
//
// A forecast is a range, never a point. The width comes from how wrong the
// same method would have been on this person's own past (see Backtest), so
// somebody whose training is erratic gets a wide range and is told why.
//
// It says what tends to follow, not what causes what. Nothing here claims an
// extra session makes anyone stronger; a what-if only moves the things the
// existing load model already describes (fatigue, detraining, timing).
//
// With too little history it says so and names what is missing, rather than
// drawing a line through three points.
//
// A leaf: it imports the lift leaf for sets and the fatigue model and nothing
// else, so the service, the coach context and the agent tool can all use it.
package forecast

import (
	"sort"
	"time"
)

// Day is the unit every horizon is measured in.
const Day = 24 * time.Hour

// Point is one observation of a series: an estimated max on a day, a
// weigh-in, a waist measurement.
type Point struct {
	At    time.Time
	Value float64
}

// Range is where a value is expected to land. Low <= Mid <= High.
type Range struct {
	Low  float64
	Mid  float64
	High float64
}

// Contains reports whether v landed inside the range.
func (r Range) Contains(v float64) bool { return v >= r.Low && v <= r.High }

// Statuses of one forecast series. Plain strings on the wire, so a new one
// never breaks a client that switches on them with a default.
const (
	StatusReady        = "ready"
	StatusInsufficient = "insufficient"
)

// Need is one thing missing before a series can be forecast: "3 of 6 squat
// sessions". Code names the kind of thing; the service words it.
type Need struct {
	Code string
	// Subject is what it is about when the code alone does not say: a lift's
	// name. Empty for body weight and waist.
	Subject string
	Have    int
	Want    int
}

// Need codes.
const (
	NeedLiftSessions = "lift_sessions"
	NeedLiftRecent   = "lift_recent"
	NeedWeighIns     = "weigh_ins"
	NeedWaistChecks  = "waist_checks"
)

// Offsets are the days ahead a forecast is drawn at: every day for two
// weeks, then every week out to twelve. Daily further out would claim a
// precision the method does not have.
func Offsets() []int {
	out := make([]int, 0, 25)
	for d := 0; d <= 14; d++ {
		out = append(out, d)
	}
	for w := 3; w <= 12; w++ {
		out = append(out, w*7)
	}
	return out
}

// MaxOffset is the furthest day ahead anything is forecast.
const MaxOffset = 84

// sorted returns the points oldest first, without touching the caller's slice.
func sorted(points []Point) []Point {
	out := append([]Point(nil), points...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// until is the points at or before t, oldest first. Input must be sorted.
func until(points []Point, t time.Time) []Point {
	n := sort.Search(len(points), func(i int) bool { return points[i].At.After(t) })
	return points[:n]
}

// since is the points at or after t. Input must be sorted.
func since(points []Point, t time.Time) []Point {
	n := sort.Search(len(points), func(i int) bool { return !points[i].At.Before(t) })
	return points[n:]
}

// days is a duration in days, as a float.
func days(d time.Duration) float64 { return d.Hours() / 24 }

// median of values; zero for none. Sorts a copy.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	mid := len(v) / 2
	if len(v)%2 == 1 {
		return v[mid]
	}
	return (v[mid-1] + v[mid]) / 2
}

// quantile is the q-th quantile (0..1) of values by linear interpolation;
// zero for none. Sorts a copy.
func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	if len(v) == 1 {
		return v[0]
	}
	pos := q * float64(len(v)-1)
	lo := int(pos)
	if lo >= len(v)-1 {
		return v[len(v)-1]
	}
	frac := pos - float64(lo)
	return v[lo] + frac*(v[lo+1]-v[lo])
}
