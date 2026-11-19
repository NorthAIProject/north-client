package forecast

import "math"

// Scenario is a what-if: the same forecast with the coming weeks changed.
//
// It changes little, on purpose. Time away pauses progress; a lighter week
// or an extra session changes how fatigued each muscle is on each day, by
// the load model that draws the body map. Nothing here says more training
// makes anyone stronger, because nothing in this person's data can show
// that it would.
type Scenario struct {
	// SkipWeeks is how many weeks, from today, with no training. 0 to
	// MaxSkipWeeks.
	SkipWeeks int
	// ExtraSessions adds this many sessions a week to the plan. 0 or 1.
	ExtraSessions int
	// DeloadNow makes this week a lighter one: fewer sets of everything.
	DeloadNow bool
}

// Scenario limits.
const (
	MaxSkipWeeks     = 4
	MaxExtraSessions = 1
)

// Clamp brings a scenario read off a query string inside its limits.
func (s Scenario) Clamp() Scenario {
	s.SkipWeeks = min(max(s.SkipWeeks, 0), MaxSkipWeeks)
	s.ExtraSessions = min(max(s.ExtraSessions, 0), MaxExtraSessions)
	return s
}

// IsZero reports the plain forecast: nothing changed.
func (s Scenario) IsZero() bool { return s == Scenario{} }

// Pause is the scenario's time away from training.
func (s Scenario) Pause() Pause { return Pause{Days: s.SkipWeeks * 7} }

// Pause is a stretch of days from today with no training.
type Pause struct {
	Days int
}

// strength is how a pause changes a lift's forecast: while away the trend
// does not advance, because progress comes from sessions and there are none.
// The forecast is the plain one, that many days late.
//
// It claims no loss of strength. A what-if is at most MaxSkipWeeks long, and
// over a break that short the evidence is that maximal strength is largely
// kept (Bosquet et al. 2013, "Effect of training cessation on muscular
// performance: a meta-analysis", doi:10.1111/sms.12047, which finds the
// loss growing with the length of the break). The body map's curve
// (lift.Strength) shades a figure; multiplied into kilograms it would take
// a third off a squat in four weeks, which nobody's logs support. If
// MaxSkipWeeks is ever raised well past four, this needs a sourced rate.
//
// A lift already sliding keeps sliding: a break is never shown as helping.
func (p Pause) strength() adjust {
	if p.Days <= 0 {
		return nil
	}
	return func(offsetDays int, ahead func(days float64) float64) float64 {
		away := min(offsetDays, p.Days)
		return math.Min(ahead(float64(offsetDays-away)), ahead(float64(offsetDays)))
	}
}
