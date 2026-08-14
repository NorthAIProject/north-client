package dashboard

import "math"

// Delta compares a metric in the selected window against the same metric in
// the window immediately before it.
//
// HasPrior is the field that matters. A person in their first week has nothing
// to compare against, and a dashboard that answers that with "+100%" on every
// tile is lying to them. When HasPrior is false the template renders nothing
// at all rather than a fabricated gain.
type Delta struct {
	Current float64
	Prior   float64

	// Pct is the change from Prior to Current, e.g. 12.5 for a 12.5% rise.
	// Meaningless unless HasPrior.
	Pct float64

	// Direction is -1, 0, or +1. Charted as a down arrow, an em dash, or an
	// up arrow — never as a colour alone, since "more" is good for water and
	// bad for nothing in particular.
	Direction int

	HasPrior bool
}

// computeDelta measures current against prior.
//
// A prior of zero yields HasPrior=false: there is no percentage change from
// nothing to something, and the honest thing to show is the absolute number
// on its own.
func computeDelta(current, prior float64) Delta {
	d := Delta{Current: current, Prior: prior}

	if prior == 0 {
		return d
	}

	d.HasPrior = true
	d.Pct = (current - prior) / math.Abs(prior) * 100

	switch {
	case current > prior:
		d.Direction = 1
	case current < prior:
		d.Direction = -1
	}
	return d
}

// Deltas is one comparison per headline metric on the overview.
type Deltas struct {
	Hydration   Delta
	SleepHours  Delta
	Calories    Delta
	CheckIns    Delta
	HabitsKept  Delta
	Journalings Delta
}
