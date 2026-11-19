package forecast

import "time"

// States of a lift's progress. Plain strings on the wire.
const (
	// ProgressProgressing: the estimated max is rising.
	ProgressProgressing = "progressing"
	// ProgressHolding: flat, but a recent best says it is not stuck.
	ProgressHolding = "holding"
	// ProgressPlateau: flat, and no better than a month ago.
	ProgressPlateau = "plateau"
	// ProgressSliding: lower than a month ago and still falling.
	ProgressSliding = "sliding"
	// ProgressInsufficient: not trained often enough lately to say.
	ProgressInsufficient = "insufficient"
)

// Plateau thresholds.
const (
	plateauWindow      = 6 * 7 * Day
	plateauMinSessions = 6
	plateauHalf        = 4 * 7 * Day
	// risingPerWeek is the slope, in percent of the max per week, that
	// counts as progress. A quarter of a percent a week is 1 kg on a 100 kg
	// lift in a month: about the smallest change a barbell can show.
	risingPerWeek = 0.25
	// A best within 1 % of the earlier best is the same lift on a different
	// day, not an improvement.
	sameBest = 1.01
	lostBest = 0.97
)

// Progress reads the state of one lift from its per-day maxes.
//
// Two things are compared: the best of the last four weeks against the best
// of the four before, and the slope over all eight. A plateau needs both to
// agree, because either alone lies: one good day hides a flat trend, and a
// flat trend after one early spike hides steady work.
func Progress(points []Point, now time.Time) string {
	pts := until(sorted(points), now)
	if len(since(pts, now.Add(-plateauWindow))) < plateauMinSessions {
		return ProgressInsufficient
	}

	recent := since(pts, now.Add(-plateauHalf))
	earlier := until(since(pts, now.Add(-2*plateauHalf)), now.Add(-plateauHalf))
	trend, ok := Fit(since(pts, now.Add(-2*plateauHalf)))
	if !ok || len(recent) == 0 {
		return ProgressInsufficient
	}
	rising := trend.PercentPerWeek() >= risingPerWeek
	if len(earlier) == 0 {
		if rising {
			return ProgressProgressing
		}
		return ProgressHolding
	}

	now4, before4 := best(recent), best(earlier)
	switch {
	case rising:
		return ProgressProgressing
	case now4 < before4*lostBest && trend.SlopePerDay < 0:
		return ProgressSliding
	case now4 <= before4*sameBest:
		return ProgressPlateau
	default:
		return ProgressHolding
	}
}

func best(points []Point) float64 {
	out := 0.0
	for _, p := range points {
		if p.Value > out {
			out = p.Value
		}
	}
	return out
}

// Stalled reports a state that counts toward a lighter week.
func Stalled(progress string) bool {
	return progress == ProgressPlateau || progress == ProgressSliding
}
