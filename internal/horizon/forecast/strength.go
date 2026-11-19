package forecast

import (
	"sort"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// Lift thresholds. A forecast needs a lift trained often enough, recently
// enough, that a trend in it is about the person and not about chance.
const (
	// liftWindow is how far back a lift's trend looks.
	liftWindow = 12 * 7 * Day
	// LiftMinSessions is how many sessions of a lift it takes.
	LiftMinSessions = 6
	// liftMinSpan is how long those sessions must cover.
	liftMinSpan = 28 * Day
	// liftRecent is how recently the lift must have been trained. After
	// three weeks away the trend describes somebody fresher.
	liftRecent = 21 * Day
	// MaxLifts is how many lifts are forecast: the ones trained most.
	MaxLifts = 6
	// maxRepsForE1RM: an estimated max from a set of twenty says more about
	// endurance than strength, and Epley's formula drifts badly up there.
	maxRepsForE1RM = 12
	// maxRIRCredit caps the reps in reserve added to a set. "Five left" is a
	// guess; crediting all of them would let a guess set the trend.
	maxRIRCredit = 4
)

// AdjustedE1RM is a set's estimated one-rep max, crediting the reps the
// person said they had left: 100 kg for 5 with 2 in reserve is treated as
// 100 kg for 7. Without that, a deliberately easy week reads as a loss of
// strength.
//
// False for a set that says nothing about maximal strength: a warm-up, a
// bodyweight set, or one past maxRepsForE1RM reps.
func AdjustedE1RM(s lift.Set) (float64, bool) {
	if !s.Counts() || s.WeightKg <= 0 || s.Reps <= 0 || s.Reps > maxRepsForE1RM {
		return 0, false
	}
	reps := s.Reps
	if s.RIR != nil && *s.RIR > 0 {
		reps += min(*s.RIR, maxRIRCredit)
	}
	return lift.E1RM(s.WeightKg, reps), true
}

// LiftHistory is one lift's sets reduced to a series: the best adjusted max
// of each day it was trained.
type LiftHistory struct {
	Key  string
	Slug string
	Name string
	// Points is one per training day, oldest first.
	Points []Point
}

// Sessions is how many days the lift was trained on or after since.
func (h LiftHistory) Sessions(since time.Time) int {
	n := 0
	for _, p := range h.Points {
		if !p.At.Before(since) {
			n++
		}
	}
	return n
}

// LiftHistories groups sets by lift, most-trained first (ties by key, so the
// order is the same every time). Days are the person's own: each set's
// LogDate, which is the calendar day they logged it on.
func LiftHistories(sets []lift.Set) []LiftHistory {
	type dayKey struct {
		lift string
		day  time.Time
	}
	best := map[dayKey]float64{}
	at := map[dayKey]time.Time{}
	meta := map[string]*LiftHistory{}
	for _, s := range sets {
		value, ok := AdjustedE1RM(s)
		if !ok {
			continue
		}
		key := s.Key()
		if key == "" {
			continue
		}
		if _, seen := meta[key]; !seen {
			meta[key] = &LiftHistory{Key: key, Slug: s.ExerciseSlug, Name: s.ExerciseName}
		}
		k := dayKey{lift: key, day: dayOf(s)}
		if value > best[k] {
			best[k] = value
		}
		if s.PerformedAt.After(at[k]) {
			at[k] = s.PerformedAt
		}
	}
	for k, value := range best {
		meta[k.lift].Points = append(meta[k.lift].Points, Point{At: at[k], Value: value})
	}

	out := make([]LiftHistory, 0, len(meta))
	for _, h := range meta {
		h.Points = sorted(h.Points)
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Points) != len(out[j].Points) {
			return len(out[i].Points) > len(out[j].Points)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// dayOf is the calendar day a set belongs to: the day it was logged for when
// that is known, the day it was performed otherwise.
func dayOf(s lift.Set) time.Time {
	if !s.LogDate.IsZero() {
		return s.LogDate
	}
	y, m, d := s.PerformedAt.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, s.PerformedAt.Location())
}

// liftFit is the fitter for a lift: the last twelve weeks, and only when
// they hold enough sessions over a long enough stretch. The same rule runs
// at every past origin of the backtest, so the record is of forecasts that
// would really have been made.
func liftFit(known []Point) (Trend, bool) {
	if len(known) == 0 {
		return Trend{}, false
	}
	recent := since(known, known[len(known)-1].At.Add(-liftWindow))
	if len(recent) < LiftMinSessions {
		return Trend{}, false
	}
	if recent[len(recent)-1].At.Sub(recent[0].At) < liftMinSpan {
		return Trend{}, false
	}
	return Fit(recent)
}

// LiftForecast is one lift's forecast and what state its progress is in.
type LiftForecast struct {
	Key  string
	Slug string
	Name string
	Series
	// Plateau is one of the Progress* states.
	Plateau string
}

// ForecastLift forecasts one lift's estimated max. pause, when set, is the
// what-if's time away from training.
func ForecastLift(h LiftHistory, now time.Time, pause Pause) LiftForecast {
	out := LiftForecast{Key: h.Key, Slug: h.Slug, Name: h.Name, Plateau: ProgressInsufficient}
	pts := until(h.Points, now)

	recent := since(pts, now.Add(-liftWindow))
	if len(recent) < LiftMinSessions || (len(recent) > 0 && recent[len(recent)-1].At.Sub(recent[0].At) < liftMinSpan) {
		out.Series = insufficient(Need{Code: NeedLiftSessions, Subject: h.Name, Have: len(recent), Want: LiftMinSessions})
		return out
	}
	last := recent[len(recent)-1].At
	if now.Sub(last) > liftRecent {
		out.Series = insufficient(Need{Code: NeedLiftRecent, Subject: h.Name, Have: 0, Want: 1})
		return out
	}

	series, ok := build(pts, now, liftFit, pause.strength())
	if !ok {
		out.Series = insufficient(Need{Code: NeedLiftSessions, Subject: h.Name, Have: len(recent), Want: LiftMinSessions})
		return out
	}
	out.Series = series
	out.Plateau = Progress(pts, now)
	return out
}

// LiftMisses replays the lift's four-week forecasts against what happened,
// oldest first: the points behind its track record.
func LiftMisses(h LiftHistory, now time.Time) []Miss {
	return Misses(until(h.Points, now), now, recordHorizonDays, liftFit)
}
