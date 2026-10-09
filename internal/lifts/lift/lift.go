// Package lift holds a lifted set and the arithmetic strength training is
// read by: estimated one-rep max, volume, and personal records.
package lift

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
)

// The kinds of set. A warm-up is logged so the session reads as it happened,
// but it is preparation rather than training: it never counts toward volume,
// records, exercise bests or fatigue. A drop set is work.
const (
	KindWarmup = "warmup"
	KindWork   = "work"
	KindDrop   = "drop"
)

// Kinds are the valid set kinds, in the order a form offers them.
var Kinds = []string{KindWork, KindWarmup, KindDrop}

// ValidKind reports whether kind is one of Kinds.
func ValidKind(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// MaxRIR bounds reps in reserve. Past ten nobody can tell.
const MaxRIR = 10

// Set is one logged set.
type Set struct {
	ID                uuid.UUID
	ActivitySessionID *uuid.UUID
	LogDate           time.Time
	ExerciseSlug      string
	ExerciseName      string
	SetNumber         int
	WeightKg          float64
	Reps              int
	PerformedAt       time.Time
	// Kind is one of Kinds; KindWork for every set logged before kinds.
	Kind string
	// RIR is reps in reserve, 0 meaning to failure; nil when nobody said.
	RIR *int
}

// Counts reports whether the set is training: anything but a warm-up.
func (s Set) Counts() bool { return s.Kind != KindWarmup }

// Working is the sets that count, in their order.
func Working(sets []Set) []Set {
	out := make([]Set, 0, len(sets))
	for _, s := range sets {
		if s.Counts() {
			out = append(out, s)
		}
	}
	return out
}

// Key is what makes two sets the same exercise: the catalog slug when there
// is one, the name as typed otherwise, so "Bench press" and "bench press "
// count together.
func (s Set) Key() string { return KeyFor(s.ExerciseSlug, s.ExerciseName) }

// KeyFor is Key for a slug and a name not yet in a Set.
func KeyFor(slug, name string) string {
	if slug != "" {
		return slug
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// Volume is weight times reps. A bodyweight set has no volume: its load is
// the person, which is not what this measures. Nor does a warm-up.
func (s Set) Volume() float64 {
	if !s.Counts() {
		return 0
	}
	return s.WeightKg * float64(s.Reps)
}

// E1RM is the set's estimated one-rep max by Epley's formula, the one most
// training apps use. It is least reliable past about ten reps, so those sets
// still count but rarely set a record.
func (s Set) E1RM() float64 { return E1RM(s.WeightKg, s.Reps) }

// E1RM for a weight and reps. A single is its own max.
func E1RM(weightKg float64, reps int) float64 {
	if reps <= 1 {
		return weightKg
	}
	return weightKg * (1 + float64(reps)/30)
}

// Record is a set that beat every earlier estimated max for its exercise.
type Record struct {
	Set      Set
	E1RM     float64
	Previous float64 // 0 for the first time the exercise was logged
}

// Records walks sets oldest first and returns each that raised its
// exercise's best estimated max. The first set of an exercise is its
// baseline, not a record. Bodyweight sets and warm-ups never count.
func Records(sets []Set) []Record {
	sorted := Working(sets)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].PerformedAt.Before(sorted[j].PerformedAt) })
	best := map[string]float64{}
	var out []Record
	for _, s := range sorted {
		if s.WeightKg <= 0 {
			continue
		}
		e := s.E1RM()
		prev, seen := best[s.Key()]
		if seen && e > prev+0.05 {
			out = append(out, Record{Set: s, E1RM: util.RoundHalfUpToScale(e, 1), Previous: util.RoundHalfUpToScale(prev, 1)})
		}
		if !seen || e > prev {
			best[s.Key()] = e
		}
	}
	return out
}

// Exercise is one exercise over a window.
type Exercise struct {
	Key      string
	Name     string
	Slug     string
	Sets     int
	Reps     int
	VolumeKg float64
	// Best weight moved for any reps, and the best estimated max.
	BestWeightKg float64
	BestE1RM     float64
	LastOn       time.Time
	// Trend is the best estimated max on each day it was trained, oldest first.
	Trend []DayValue
}

// DayValue is one figure for one day.
type DayValue struct {
	Day   time.Time
	Value float64
}

// ByExercise groups working sets per exercise, most trained (by sets)
// first. Names come from the latest set, so a rename shows its newest
// spelling. Warm-ups are left out.
func ByExercise(sets []Set) []Exercise {
	index := map[string]int{}
	var out []Exercise
	days := map[string]map[time.Time]float64{}
	for _, s := range Working(sets) {
		i, ok := index[s.Key()]
		if !ok {
			i = len(out)
			index[s.Key()] = i
			out = append(out, Exercise{Key: s.Key(), Slug: s.ExerciseSlug})
			days[s.Key()] = map[time.Time]float64{}
		}
		e := &out[i]
		if !s.PerformedAt.Before(e.LastOn) {
			e.LastOn = s.PerformedAt
			e.Name = s.ExerciseName
		}
		e.Sets++
		e.Reps += s.Reps
		e.VolumeKg += s.Volume()
		e.BestWeightKg = math.Max(e.BestWeightKg, s.WeightKg)
		e.BestE1RM = math.Max(e.BestE1RM, s.E1RM())
		if s.E1RM() > days[s.Key()][s.LogDate] {
			days[s.Key()][s.LogDate] = s.E1RM()
		}
	}
	for i := range out {
		e := &out[i]
		e.VolumeKg = util.RoundHalfUpToScale(e.VolumeKg, 1)
		e.BestE1RM = util.RoundHalfUpToScale(e.BestE1RM, 1)
		for day, v := range days[e.Key] {
			e.Trend = append(e.Trend, DayValue{Day: day, Value: util.RoundHalfUpToScale(v, 1)})
		}
		sort.Slice(e.Trend, func(a, b int) bool { return e.Trend[a].Day.Before(e.Trend[b].Day) })
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Sets != out[b].Sets {
			return out[a].Sets > out[b].Sets
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// Workouts counts distinct workouts: sets sharing an activity session are
// one, and sets without one group by their day.
func Workouts(sets []Set) int {
	seen := map[string]bool{}
	for _, s := range sets {
		key := s.LogDate.Format(time.DateOnly)
		if s.ActivitySessionID != nil {
			key = s.ActivitySessionID.String()
		}
		seen[key] = true
	}
	return len(seen)
}

// WeeklyVolume sums volume per ISO week (Monday start), oldest first, with
// empty weeks between since and until included so a chart shows the gaps.
func WeeklyVolume(sets []Set, since, until time.Time) []DayValue {
	start := weekStart(since)
	totals := map[time.Time]float64{}
	for _, s := range sets {
		totals[weekStart(s.LogDate)] += s.Volume()
	}
	var out []DayValue
	for w := start; w.Before(until); w = w.AddDate(0, 0, 7) {
		out = append(out, DayValue{Day: w, Value: util.RoundHalfUpToScale(totals[w], 1)})
	}
	return out
}

func weekStart(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -offset)
}

// LastWorkout returns the sets of the most recent workout in sets (which
// must be one exercise's), ordered by set number. It is what a new workout
// prefills from.
func LastWorkout(sets []Set) []Set {
	if len(sets) == 0 {
		return nil
	}
	latest := sets[0]
	for _, s := range sets {
		if s.PerformedAt.After(latest.PerformedAt) {
			latest = s
		}
	}
	var out []Set
	for _, s := range sets {
		if sameWorkout(s, latest) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SetNumber != out[j].SetNumber {
			return out[i].SetNumber < out[j].SetNumber
		}
		return out[i].PerformedAt.Before(out[j].PerformedAt)
	})
	return out
}

func sameWorkout(a, b Set) bool {
	if a.ActivitySessionID != nil || b.ActivitySessionID != nil {
		return a.ActivitySessionID != nil && b.ActivitySessionID != nil && *a.ActivitySessionID == *b.ActivitySessionID
	}
	return a.LogDate.Equal(b.LogDate)
}

// Summary renders recent lifting for the coach.
func Summary(sets []Set, records []Record) string {
	if len(sets) == 0 {
		return "Lifting: no sets logged in the last 14 days."
	}
	var b strings.Builder
	total := 0.0
	for _, s := range sets {
		total += s.Volume()
	}
	fmt.Fprintf(&b, "Lifting, last 14 days: %d workouts, %d working sets, %.0f kg total volume.", Workouts(sets), len(Working(sets)), total)
	for i, e := range ByExercise(sets) {
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, " %s: %d sets, best %.1f kg, est. 1RM %.1f kg.", e.Name, e.Sets, e.BestWeightKg, e.BestE1RM)
	}
	for _, r := range records {
		fmt.Fprintf(&b, " New record on %s: %s %.1f kg x %d (est. 1RM %.1f kg).",
			r.Set.LogDate.Format("Jan 2"), r.Set.ExerciseName, r.Set.WeightKg, r.Set.Reps, r.E1RM)
	}
	return b.String()
}
