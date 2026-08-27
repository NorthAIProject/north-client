// Package exercise holds the shape of a catalog exercise.
//
// A leaf, so the exercises service, the workouts service that picks from the
// catalog, and the templates that render one can all import it without
// importing each other. See CLAUDE.md on slice layout.
package exercise

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Categories the catalog uses. These are the source dataset's own words rather
// than a taxonomy North invented, and the database column is unconstrained to
// match — see migrations/00020.
const (
	CategoryStrength             = "strength"
	CategoryCardio               = "cardio"
	CategoryStretching           = "stretching"
	CategoryPlyometrics          = "plyometrics"
	CategoryPowerlifting         = "powerlifting"
	CategoryOlympicWeightlifting = "olympic_weightlifting"
	CategoryStrongman            = "strongman"
)

// Categories is the ordered set offered as a browse filter.
var Categories = []string{
	CategoryStrength,
	CategoryCardio,
	CategoryStretching,
	CategoryPlyometrics,
	CategoryPowerlifting,
	CategoryOlympicWeightlifting,
	CategoryStrongman,
}

const (
	DifficultyBeginner     = "beginner"
	DifficultyIntermediate = "intermediate"
	DifficultyExpert       = "expert"
)

// Difficulties is ordered easiest-first, which is the order a browse filter
// should offer them in.
var Difficulties = []string{DifficultyBeginner, DifficultyIntermediate, DifficultyExpert}

// EquipmentNone marks an exercise that needs nothing at all.
const EquipmentNone = "none"

// EquipmentOther marks equipment North has no rule for — sleds, tyres, ropes,
// loose plates. Deliberately not folded into EquipmentNone: telling someone
// training in a bedroom that a tyre flip needs no equipment is worse than
// telling them nothing.
const EquipmentOther = "other"

// Exercise is one catalog entry.
//
// Its reason for existing is Primary/Secondary. Before the catalog, the AI
// invented an exercise name and assigned its own muscle keys, and the 3D
// viewer highlighted whatever that one call guessed. These fields are a fixed
// answer instead.
type Exercise struct {
	ID   uuid.UUID
	Slug string
	Name string

	Category   string
	Equipment  string
	Difficulty string

	Instructions string
	VideoURL     string

	// Muscle keys from internal/workouts/plan.MuscleGroups.
	Primary   []string
	Secondary []string

	// IllustrationSlug names the directory under web/assets/exercises holding
	// this movement's three pose frames, or is empty when there is no artwork
	// for it.
	//
	// Its own field rather than a reuse of Slug because the two vocabularies
	// disagree: the catalog came from FitMe and calls the bench press
	// "barbell-bench-press-medium-grip", where the artwork's source calls it
	// "bench-press". See migrations/20260827150000 and scripts/workout-guide-art.
	IllustrationSlug string
}

// HasIllustration reports whether this exercise has pose artwork to render.
//
// Most of the catalog does not: the artwork covers 302 movements and the
// catalog carries more, so an absent illustration is ordinary rather than a
// missing file.
func (e Exercise) HasIllustration() bool {
	return e.IllustrationSlug != ""
}

// NeedsEquipment reports whether this exercise requires anything to perform.
func (e Exercise) NeedsEquipment() bool {
	return e.Equipment != "" && e.Equipment != EquipmentNone
}

// Line renders the exercise as one line for a prompt's candidate list.
//
// Kept here rather than in the workouts service so the format the model is
// shown and the data it is derived from stay in one place: a field added to
// Exercise that the model should see is added here, not hunted for elsewhere.
func (e Exercise) Line() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s — %s", e.Slug, e.Name)
	if e.NeedsEquipment() {
		fmt.Fprintf(&b, " (%s)", e.Equipment)
	}
	if len(e.Primary) > 0 {
		fmt.Fprintf(&b, " [%s]", strings.Join(e.Primary, ", "))
	}

	return b.String()
}
