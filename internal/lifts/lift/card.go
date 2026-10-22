package lift

import (
	"fmt"
	"sort"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// CardSession is the workout a card is about, whatever kind it was: a
// lifting session, a run, an imported ride.
type CardSession struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Name         string // the activity's name, "Weight training"
	ActivityCode string
	StartedAt    time.Time
	EndedAt      time.Time
	Duration     time.Duration // the same elapsed figure the finish screen shows
	DistanceM    *float64
}

// CardExercise is one movement on a card. Only working sets count.
type CardExercise struct {
	Name     string
	Sets     int
	VolumeKg float64
	// Best is the heaviest set in English, "22.5 kg × 8", empty for a
	// bodyweight-only movement. A person reads BestIn.
	Best         string
	BestWeightKg float64
	BestReps     int
	// PR is true when a set of this workout raised the person's best
	// estimated max. The first time an exercise is done is a baseline.
	PR bool
}

// Card is a finished workout for someone else to read: what it was, how long
// it took and what was lifted. It carries no calories, heart rate or body
// numbers.
type Card struct {
	CardSession
	Sets      int
	VolumeKg  float64
	PRs       int            // exercises with PR set
	Exercises []CardExercise // most volume first, then by name
}

// BuildCard summarises one workout. current is the session's sets; earlier is
// the same person's sets of the same exercises before it, which is what makes
// a record a record. Warm-ups are left out throughout.
func BuildCard(s CardSession, current, earlier []Set) Card {
	current = Working(current)
	card := Card{CardSession: s, Exercises: []CardExercise{}}

	inThisWorkout := make(map[uuid.UUID]bool, len(current))
	for _, set := range current {
		inThisWorkout[set.ID] = true
	}
	prs := map[string]bool{}
	all := append(append(make([]Set, 0, len(earlier)+len(current)), earlier...), current...)
	for _, r := range Records(all) {
		if inThisWorkout[r.Set.ID] {
			prs[r.Set.Key()] = true
		}
	}

	for _, sets := range groupInOrder(current) {
		e := cardExercise(sets)
		e.PR = prs[sets[0].Key()]
		card.Sets += e.Sets
		card.VolumeKg += e.VolumeKg
		if e.PR {
			card.PRs++
		}
		card.Exercises = append(card.Exercises, e)
	}
	card.VolumeKg = util.RoundHalfUpToScale(card.VolumeKg, 1)
	sort.SliceStable(card.Exercises, func(i, j int) bool {
		a, b := card.Exercises[i], card.Exercises[j]
		if a.VolumeKg != b.VolumeKg {
			return a.VolumeKg > b.VolumeKg
		}
		return a.Name < b.Name
	})
	return card
}

func cardExercise(sets []Set) CardExercise {
	e := CardExercise{Name: sets[len(sets)-1].ExerciseName, Sets: len(sets)}
	var best Set
	var haveBest bool
	for _, s := range sets {
		e.VolumeKg += s.Volume()
		if s.WeightKg <= 0 {
			continue
		}
		if !haveBest || s.E1RM() > best.E1RM() {
			best, haveBest = s, true
		}
	}
	e.VolumeKg = util.RoundHalfUpToScale(e.VolumeKg, 1)
	if haveBest {
		e.BestWeightKg, e.BestReps = best.WeightKg, best.Reps
		e.Best = e.BestIn(i18n.DefaultLocale)
	}
	return e
}

// BestIn is the heaviest set with tag's decimal mark: "22,5 kg × 8" in
// Spanish. Empty for a bodyweight-only movement.
func (e CardExercise) BestIn(tag string) string {
	if e.BestWeightKg <= 0 {
		return ""
	}
	return fmt.Sprintf("%s × %d", formatKg(tag, e.BestWeightKg), e.BestReps)
}
