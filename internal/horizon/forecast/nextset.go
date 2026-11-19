package forecast

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// Next-set settings.
const (
	// nextMinSessions is how many earlier sessions of a lift it takes
	// before a load is suggested. One session is a first try, not a level.
	nextMinSessions = 2
	// maxJump caps a suggestion over the last session's top load.
	maxJump = 1.10
	// easierDay scales the load on a lighter day or in a lighter week.
	easierDay = 0.9
	// DefaultIncrementKg is the smallest step most gyms can load.
	DefaultIncrementKg = 2.5
	defaultRepsLow     = 8
	defaultRepsHigh    = 12
)

// Reasons for a suggestion. Codes; the service words them.
const (
	NextAddLoad = "add_load" // every set reached the top of the range with reps to spare
	NextAddRep  = "add_rep"  // same load, one more rep
	NextRepeat  = "repeat"   // fell under the range: same load, bottom of the range
	NextLighter = "lighter"  // a lighter day or week
)

// RepRange is the reps a set is meant to fall in.
type RepRange struct {
	Low  int
	High int
}

var repsPattern = regexp.MustCompile(`(\d+)\s*(?:-|–|—|to)\s*(\d+)|(\d+)`)

// ParseReps reads a plan's rep text: "8-12", "8–12", "5", "3x8-10" (the
// part after the x), "AMRAP" (no range). False when there is no number.
func ParseReps(text string) (RepRange, bool) {
	if i := lastIndexAny(text, "xX×"); i >= 0 {
		text = text[i+1:]
	}
	m := repsPattern.FindStringSubmatch(text)
	if m == nil {
		return RepRange{}, false
	}
	if m[1] != "" {
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		if lo > hi {
			lo, hi = hi, lo
		}
		if lo < 1 || hi > 100 {
			return RepRange{}, false
		}
		return RepRange{Low: lo, High: hi}, true
	}
	n, _ := strconv.Atoi(m[3])
	if n < 1 || n > 100 {
		return RepRange{}, false
	}
	return RepRange{Low: n, High: n}, true
}

func lastIndexAny(s, chars string) int {
	last := -1
	for i, r := range s {
		for _, c := range chars {
			if r == c {
				last = i
			}
		}
	}
	return last
}

// NextSetInput is what one lift's suggestion is read from.
type NextSetInput struct {
	// Sets are the lift's logged sets, any order; only this lift's.
	Sets []lift.Set
	// Range is the plan's rep range; zero means the default 8 to 12.
	Range RepRange
	// IncrementKg is the smallest load step; zero means the default.
	IncrementKg float64
	// Lighter is true on a lighter day or in a lighter week.
	Lighter bool
	Now     time.Time
}

// NextSet is the suggested first working set.
type NextSet struct {
	WeightKg float64
	Reps     int
	Reason   string
	// LastWeightKg and LastReps are the set it was worked out from: the
	// last session's top set.
	LastWeightKg float64
	LastReps     int
}

// SuggestNext suggests the next working set by double progression: add reps
// at a load until every set reaches the top of the range with something in
// reserve, then add load and start again at the bottom.
//
// False when the lift has fewer than two earlier sessions, or was only ever
// done without load. It is a starting point for the set-entry form, never
// more than a tenth above the last session.
func SuggestNext(in NextSetInput) (NextSet, bool) {
	rng := in.Range
	if rng.Low <= 0 || rng.High < rng.Low {
		rng = RepRange{Low: defaultRepsLow, High: defaultRepsHigh}
	}
	step := in.IncrementKg
	if step <= 0 {
		step = DefaultIncrementKg
	}

	sessions := sessionsOf(in.Sets, in.Now)
	if len(sessions) < nextMinSessions {
		return NextSet{}, false
	}
	last := sessions[len(sessions)-1]
	top := last[0]
	for _, s := range last {
		if s.WeightKg > top.WeightKg {
			top = s
		}
	}
	if top.WeightKg <= 0 {
		return NextSet{}, false
	}

	// Judge the sets done at the top load: back-off sets are meant to be
	// lighter and say nothing about whether the top load is mastered.
	allAtTop, minReps, spare := true, top.Reps, true
	for _, s := range last {
		if s.WeightKg != top.WeightKg {
			continue
		}
		if s.Reps < rng.High {
			allAtTop = false
		}
		if s.Reps < minReps {
			minReps = s.Reps
		}
		// Nobody said how hard it was: do not assume there was more.
		if s.RIR == nil || *s.RIR < 1 {
			spare = false
		}
	}

	out := NextSet{LastWeightKg: top.WeightKg, LastReps: top.Reps}
	switch {
	case in.Lighter:
		out.WeightKg, out.Reps, out.Reason = roundTo(top.WeightKg*easierDay, step), rng.Low, NextLighter
	case allAtTop && spare:
		out.WeightKg, out.Reps, out.Reason = top.WeightKg+step, rng.Low, NextAddLoad
	case minReps < rng.Low:
		out.WeightKg, out.Reps, out.Reason = top.WeightKg, rng.Low, NextRepeat
	default:
		out.WeightKg, out.Reps, out.Reason = top.WeightKg, min(minReps+1, rng.High), NextAddRep
	}
	if limit := top.WeightKg * maxJump; out.WeightKg > limit && out.Reason == NextAddLoad {
		// A step bigger than a tenth of the load (2.5 kg on a 20 kg lift):
		// earn it with reps instead.
		out.WeightKg, out.Reps, out.Reason = top.WeightKg, min(top.Reps+1, rng.High+2), NextAddRep
	}
	return out, true
}

// sessionsOf groups a lift's working, loaded-or-not sets into days, oldest
// first, ignoring anything after now.
func sessionsOf(sets []lift.Set, now time.Time) [][]lift.Set {
	byDay := map[time.Time][]lift.Set{}
	for _, s := range lift.Working(sets) {
		if s.PerformedAt.After(now) || s.Reps <= 0 {
			continue
		}
		day := dayOf(s)
		byDay[day] = append(byDay[day], s)
	}
	days := make([]time.Time, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	out := make([][]lift.Set, len(days))
	for i, d := range days {
		out[i] = byDay[d]
	}
	return out
}

func roundTo(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return math.Round(v/step) * step
}
