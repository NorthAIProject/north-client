package lift

import (
	"math"
	"sort"
	"time"
)

// Heat is "what did I train lately" for the body figure, which is a different
// question from fatigue. Fatigue fades in a day and a half because it is about
// whether a muscle can go again; heat holds for a week because it is about
// what this week's training has covered.
const (
	// HeatDayFactor is how much one day of age keeps of a set's heat from the
	// second day on: sets from today or yesterday count fully, three days ago
	// about 0.6, seven days ago about 0.2.
	HeatDayFactor = 0.775
	// heatScale is the decayed set count that puts a muscle at about 0.63
	// (1 − 1/e): four working sets yesterday is a session for that muscle.
	heatScale = 4.0
)

// MuscleHeat is one muscle on the figure.
type MuscleHeat struct {
	Muscle string
	// Intensity is 0 (nothing in the window) to 1.
	Intensity float64
	// LastTrained is the latest working set for the muscle at all, inside the
	// window or not.
	LastTrained time.Time
}

// HeatMap is every muscle the sets reach.
type HeatMap struct {
	Days int
	// Muscles are sorted by key.
	Muscles []MuscleHeat
	// LastSession is the latest working set of any kind; zero with none.
	LastSession time.Time
}

// HeatDecay is the weight of a set done ageDays calendar days ago.
func HeatDecay(ageDays int) float64 {
	if ageDays <= 1 {
		return 1
	}
	return math.Pow(HeatDayFactor, float64(ageDays-1))
}

// HeatOf reads working sets into per-muscle heat at now. A set's age is
// counted in calendar days where the person is (loc), so a set at 23:00
// yesterday is "yesterday", not "today". Sets more than days old add no heat
// but still date LastTrained; warm-ups count for nothing.
func HeatOf(sets []Set, weights Weights, now time.Time, loc *time.Location, days int) HeatMap {
	out := HeatMap{Days: days}
	today := civilDay(now, loc)
	sum := map[string]float64{}
	last := map[string]time.Time{}
	for _, s := range Working(sets) {
		if s.PerformedAt.After(now) {
			continue
		}
		if s.PerformedAt.After(out.LastSession) {
			out.LastSession = s.PerformedAt
		}
		age := daysBetween(civilDay(s.PerformedAt, loc), today)
		for m, w := range weights[s.ExerciseSlug] {
			if s.PerformedAt.After(last[m]) {
				last[m] = s.PerformedAt
			}
			if age <= days {
				sum[m] += w * HeatDecay(age)
			}
		}
	}
	for m, at := range last {
		out.Muscles = append(out.Muscles, MuscleHeat{
			Muscle:      m,
			Intensity:   1 - math.Exp(-sum[m]/heatScale),
			LastTrained: at,
		})
	}
	sort.Slice(out.Muscles, func(i, j int) bool { return out.Muscles[i].Muscle < out.Muscles[j].Muscle })
	return out
}

// civilDay is t's calendar date in loc, as midnight UTC so that subtracting
// two of them is a whole number of days whatever the DST.
func civilDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}
