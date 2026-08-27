package plan

import (
	"fmt"
	"strings"
)

// Editing a plan. Pure functions: a plan in, a new plan out, no database and no
// context.
//
// They live here rather than on the service because what an edit preserves is a
// training decision, not a persistence one — and because the interesting cases
// (what a swap keeps, what a move does to everything after it) are worth
// testing without a database.
//
// Nothing here mutates its argument. A caller holding the plan it just read
// must be able to compare the two, and the service writes the result as a new
// row while the original stays exactly as it was stored.

// Movement is the part of an exercise a swap replaces: which lift it is, rather
// than how much of it to do.
//
// A separate type so the split is visible in the signature. Sets, reps, rest
// and the equipment substitute are prescription and survive a swap; everything
// here describes the movement itself and does not.
//
// Kept free of any catalog import so this package stays a leaf — the service
// maps a catalog row onto it.
type Movement struct {
	Name             string
	Equipment        string
	CatalogSlug      string
	IllustrationSlug string

	// Muscle keys from MuscleGroups.
	Primary   []string
	Secondary []string
}

// Swap replaces the movement at day/index, keeping its prescription.
//
// Sets, reps, rest and the substitute stay because someone swapping a barbell
// row for a dumbbell row wants the same work, not a reset to a default they did
// not choose.
//
// Form cues and stabilizers are cleared rather than carried. Both described the
// old lift — a cue reading "keep the bar close" under a machine row is worse
// than no cue at all, and the catalog carries neither field to replace them
// with. The template renders nothing for an empty cue.
func Swap(p Plan, day, index int, with Movement) (Plan, error) {
	out, err := copyFor(p, day, index)
	if err != nil {
		return Plan{}, err
	}

	ex := &out.Days[day].Exercises[index]
	ex.Name = with.Name
	ex.Equipment = with.Equipment
	ex.CatalogSlug = with.CatalogSlug
	ex.IllustrationSlug = with.IllustrationSlug
	ex.Primary = append([]string(nil), with.Primary...)
	ex.Secondary = append([]string(nil), with.Secondary...)
	ex.FormCues = ""
	ex.Stabilizers = nil

	return out, nil
}

// What a newly added exercise starts at.
//
// The catalog describes movements, not dosage — it has no sets or reps columns
// — so there is nothing to copy and this is a choice. Chosen to be obviously
// generic rather than falsely specific: a plausible-looking 4x6 would read as
// though something had reasoned about it.
const (
	DefaultSets        = 3
	DefaultReps        = "8-12"
	DefaultRestSeconds = 90
)

// NewExercise builds an exercise for a movement being added to a plan.
//
// No form cues: the catalog carries instructions, not cues, and the ones on
// generated exercises came from the model reasoning about that specific plan.
// Inventing one here would be putting words in its mouth.
func NewExercise(m Movement) Exercise {
	return Exercise{
		Name:             m.Name,
		Sets:             DefaultSets,
		Reps:             DefaultReps,
		RestSeconds:      DefaultRestSeconds,
		Equipment:        m.Equipment,
		CatalogSlug:      m.CatalogSlug,
		IllustrationSlug: m.IllustrationSlug,
		Primary:          append([]string(nil), m.Primary...),
		Secondary:        append([]string(nil), m.Secondary...),
	}
}

// Insert adds an exercise to a day at index, shifting the rest down.
//
// index == len(exercises) appends, which is how the UI adds to the end of a
// day. Anything beyond that is out of range.
func Insert(p Plan, day, index int, ex Exercise) (Plan, error) {
	if day < 0 || day >= len(p.Days) {
		return Plan{}, fmt.Errorf("day %d is outside this plan's %d days", day, len(p.Days))
	}
	existing := p.Days[day].Exercises
	if index < 0 || index > len(existing) {
		return Plan{}, fmt.Errorf("cannot insert at %d in %s's %d exercises", index, p.Days[day].Weekday, len(existing))
	}

	out := p
	out.Days = append([]PlanDay(nil), p.Days...)

	// Built into a fresh slice rather than with append-in-place, which would
	// write through to the original's backing array when it had spare capacity.
	grown := make([]Exercise, 0, len(existing)+1)
	grown = append(grown, existing[:index]...)
	grown = append(grown, ex)
	grown = append(grown, existing[index:]...)
	out.Days[day].Exercises = grown

	return out, nil
}

// Remove drops the exercise at day/index.
//
// A day may end up empty. That is allowed: someone clearing a day out before
// rebuilding it is doing something reasonable, and refusing the last removal
// would mean the only way to replace every exercise is to add first and delete
// after. Validate has its own opinion, which is reported rather than enforced.
func Remove(p Plan, day, index int) (Plan, error) {
	out, err := copyFor(p, day, index)
	if err != nil {
		return Plan{}, err
	}

	existing := out.Days[day].Exercises
	out.Days[day].Exercises = append(existing[:index], existing[index+1:]...)
	return out, nil
}

// Move shifts the exercise at from to the position to, within one day.
//
// Order is training information — compounds before accessories, the lift you
// care about while you are fresh — so this is a real edit rather than a
// cosmetic one.
func Move(p Plan, day, from, to int) (Plan, error) {
	out, err := copyFor(p, day, from)
	if err != nil {
		return Plan{}, err
	}

	exercises := out.Days[day].Exercises
	if to < 0 || to >= len(exercises) {
		return Plan{}, fmt.Errorf("cannot move to %d in %s's %d exercises", to, p.Days[day].Weekday, len(exercises))
	}
	if from == to {
		return out, nil
	}

	moving := exercises[from]
	rest := append(exercises[:from:from], exercises[from+1:]...)
	out.Days[day].Exercises = append(rest[:to:to], append([]Exercise{moving}, rest[to:]...)...)

	return out, nil
}

// maxRepsLength bounds the free-text rep range.
//
// Reps is text because training is written that way — "8-12", "5", "AMRAP" —
// so it cannot be an integer. It is still a rep range rather than a notes
// field, and something longer than this is not one.
const maxRepsLength = 32

// SetPrescription changes how much of an exercise to do, leaving the movement
// alone. The inverse of Swap.
func SetPrescription(p Plan, day, index, sets int, reps string, restSeconds int) (Plan, error) {
	reps = strings.TrimSpace(reps)

	switch {
	case sets < 1:
		return Plan{}, fmt.Errorf("an exercise needs at least one set, not %d", sets)
	case reps == "":
		return Plan{}, fmt.Errorf("an exercise needs a rep range")
	case len(reps) > maxRepsLength:
		return Plan{}, fmt.Errorf("a rep range is at most %d characters", maxRepsLength)
	case restSeconds < 0:
		return Plan{}, fmt.Errorf("rest cannot be negative")
	}

	out, err := copyFor(p, day, index)
	if err != nil {
		return Plan{}, err
	}

	ex := &out.Days[day].Exercises[index]
	ex.Sets = sets
	ex.Reps = reps
	ex.RestSeconds = restSeconds

	return out, nil
}

// copyFor deep-copies a plan far enough that the exercise at day/index can be
// written without touching the original, and reports a bad position as an error.
//
// The indices arrive from a URL, so out of range is an ordinary request rather
// than a programming mistake, and must not panic.
func copyFor(p Plan, day, index int) (Plan, error) {
	if day < 0 || day >= len(p.Days) {
		return Plan{}, fmt.Errorf("day %d is outside this plan's %d days", day, len(p.Days))
	}
	if index < 0 || index >= len(p.Days[day].Exercises) {
		return Plan{}, fmt.Errorf("exercise %d is outside %s's %d exercises", index, p.Days[day].Weekday, len(p.Days[day].Exercises))
	}

	out := p
	out.Days = append([]PlanDay(nil), p.Days...)
	out.Days[day].Exercises = append([]Exercise(nil), p.Days[day].Exercises...)
	return out, nil
}
