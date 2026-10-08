package lift

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// How logged sets turn into per-muscle readiness. The model is deliberately
// plain — every set counts the same, whatever the load — because the coach
// needs "legs are still cooked", not a physiology simulation. Measure before
// making it cleverer.
const (
	// SecondaryWeight is how much a set counts toward a muscle that assists
	// the movement rather than drives it.
	SecondaryWeight = 0.4

	// FatigueHalfLife is how fast one set's fatigue fades.
	FatigueHalfLife = 36 * time.Hour
	// fatigueScan is how far back fatigue looks; anything older has decayed
	// below a hundredth of a set.
	fatigueScan = 30 * 24 * time.Hour
	// fatigueScale is the decayed set count that puts a muscle at about 0.63
	// (1 − 1/e): six fresh hard sets is a real session for one muscle.
	fatigueScale = 6.0

	// StrengthHold is how long strength holds after a muscle was last worked,
	// and StrengthHalfLife how fast it fades afterwards, down to strengthFloor.
	StrengthHold     = 14 * 24 * time.Hour
	StrengthHalfLife = 28 * 24 * time.Hour
	strengthFloor    = 0.5
	// detrainedBelow is about three weeks without work.
	detrainedBelow = 0.85
)

// State is how ready a muscle is to be trained again.
type State string

const (
	StateReady      State = "ready"
	StateRecovering State = "recovering"
	StateFatigued   State = "fatigued"
)

// StateOf reads a fatigue value from Fatigue.
func StateOf(fatigue float64) State {
	switch {
	case fatigue > 0.5:
		return StateFatigued
	case fatigue >= 0.25:
		return StateRecovering
	default:
		return StateReady
	}
}

// MuscleWeights is how much one set of an exercise counts toward each muscle:
// 1 for a primary mover, SecondaryWeight for an assisting one. A muscle
// listed as both is primary.
func MuscleWeights(primary, secondary []string) map[string]float64 {
	out := make(map[string]float64, len(primary)+len(secondary))
	for _, m := range secondary {
		out[m] = SecondaryWeight
	}
	for _, m := range primary {
		out[m] = 1
	}
	return out
}

// Weights is MuscleWeights per exercise slug. A set whose slug is missing
// from it — typed in, not from the catalog — has unknown muscles and counts
// toward none.
type Weights map[string]map[string]float64

// Fatigue is each muscle's fatigue at now, 0 (fresh) to 1 (spent): every set
// adds its muscle weight, halving every FatigueHalfLife. Sets after now or
// older than the scan window are ignored.
func Fatigue(sets []Set, weights Weights, now time.Time) map[string]float64 {
	raw := map[string]float64{}
	for _, s := range sets {
		age := now.Sub(s.PerformedAt)
		if age < 0 || age > fatigueScan {
			continue
		}
		decay := math.Pow(0.5, age.Hours()/FatigueHalfLife.Hours())
		for m, w := range weights[s.ExerciseSlug] {
			raw[m] += w * decay
		}
	}
	out := make(map[string]float64, len(raw))
	for m, v := range raw {
		out[m] = 1 - math.Exp(-v/fatigueScale)
	}
	return out
}

// Strength is how much of a muscle's strength is left after a gap since
// lastTrained: all of it for StrengthHold, then halving every
// StrengthHalfLife, never below half.
func Strength(lastTrained, now time.Time) float64 {
	gap := now.Sub(lastTrained) - StrengthHold
	if gap <= 0 {
		return 1
	}
	return math.Max(strengthFloor, math.Pow(0.5, gap.Hours()/StrengthHalfLife.Hours()))
}

// MuscleLoad is one muscle's readiness.
type MuscleLoad struct {
	Muscle      string
	Fatigue     float64
	State       State
	LastTrained time.Time
	Strength    float64
}

// Detrained reports whether the muscle has gone long enough without work to
// be losing strength.
func (m MuscleLoad) Detrained() bool { return m.Strength < detrainedBelow }

// Load is every muscle the sets reach, at one moment.
type Load struct {
	At time.Time
	// Muscles are the ones trained at least once, most fatigued first.
	Muscles []MuscleLoad
	// LastSession is when the latest set was done; zero with no sets.
	LastSession time.Time
	// Unmapped counts sets in the fatigue window whose exercise has no
	// catalog muscles, so their work is missing from Muscles.
	Unmapped int
}

// LoadOf reads sets (any order, any span — older ones only feed LastTrained)
// into per-muscle readiness at now.
func LoadOf(sets []Set, weights Weights, now time.Time) Load {
	load := Load{At: now}
	last := map[string]time.Time{}
	for _, s := range sets {
		if s.PerformedAt.After(now) {
			continue
		}
		if s.PerformedAt.After(load.LastSession) {
			load.LastSession = s.PerformedAt
		}
		muscles, ok := weights[s.ExerciseSlug]
		if !ok && now.Sub(s.PerformedAt) <= fatigueScan {
			load.Unmapped++
		}
		for m := range muscles {
			if s.PerformedAt.After(last[m]) {
				last[m] = s.PerformedAt
			}
		}
	}
	fatigue := Fatigue(sets, weights, now)
	for m, at := range last {
		load.Muscles = append(load.Muscles, MuscleLoad{
			Muscle:      m,
			Fatigue:     fatigue[m],
			State:       StateOf(fatigue[m]),
			LastTrained: at,
			Strength:    Strength(at, now),
		})
	}
	sort.Slice(load.Muscles, func(i, j int) bool {
		a, b := load.Muscles[i], load.Muscles[j]
		if a.Fatigue != b.Fatigue {
			return a.Fatigue > b.Fatigue
		}
		return a.Muscle < b.Muscle
	})
	return load
}

// In is the muscles in one state, most fatigued first.
func (l Load) In(state State) []MuscleLoad {
	var out []MuscleLoad
	for _, m := range l.Muscles {
		if m.State == state {
			out = append(out, m)
		}
	}
	return out
}

// Detrained is the muscles losing strength, longest gap first.
func (l Load) Detrained() []MuscleLoad {
	var out []MuscleLoad
	for _, m := range l.Muscles {
		if m.Detrained() {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastTrained.Equal(out[j].LastTrained) {
			return out[i].LastTrained.Before(out[j].LastTrained)
		}
		return out[i].Muscle < out[j].Muscle
	})
	return out
}

// ReadinessSummary renders a Load for the coach: when the last session was,
// what is still tired, and what has gone untrained long enough to fade.
func ReadinessSummary(l Load) string {
	if l.LastSession.IsZero() {
		return "Training readiness: no sets logged yet."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Training readiness: last lifting session %s (%s).",
		daysAgo(calendarDays(l.LastSession, l.At)), l.LastSession.In(l.At.Location()).Format("Mon Jan 2"))
	if fatigued := l.In(StateFatigued); len(fatigued) > 0 {
		fmt.Fprintf(&b, " Fatigued, rest these: %s.", muscleNames(fatigued))
	}
	if recovering := l.In(StateRecovering); len(recovering) > 0 {
		fmt.Fprintf(&b, " Recovering: %s.", muscleNames(recovering))
	}
	if detrained := l.Detrained(); len(detrained) > 0 {
		parts := make([]string, len(detrained))
		for i, m := range detrained {
			parts[i] = fmt.Sprintf("%s (%d days)", m.Muscle, calendarDays(m.LastTrained, l.At))
		}
		fmt.Fprintf(&b, " Untrained long enough to lose strength: %s.", strings.Join(parts, ", "))
	}
	if l.Unmapped > 0 {
		fmt.Fprintf(&b, " %d recent sets are exercises outside the catalog, so their muscles are not counted.", l.Unmapped)
	}
	return b.String()
}

// calendarDays counts midnights between from and to in to's time zone, so a
// set last night is "yesterday" even when it was only ten hours ago.
func calendarDays(from, to time.Time) int {
	y, m, d := from.In(to.Location()).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	y, m, d = to.Date()
	end := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return int(end.Sub(start).Hours() / 24)
}

func daysAgo(days int) string {
	switch days {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

func muscleNames(ms []MuscleLoad) string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Muscle
	}
	return strings.Join(names, ", ")
}
