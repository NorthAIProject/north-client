// Package exercises owns the reference catalog of movements: what each one is,
// what it needs, and which muscles it trains.
//
// The catalog exists so muscle data has a source other than the model. Before
// it, a generated plan named an exercise and assigned its own muscle keys, and
// the 3D viewer coloured whatever that single call produced. A catalog row is
// a fixed answer that a plan can be resolved against.
//
// It is read-only: seeded by migration, never written by the application. That
// is why there is no Create/Update/Delete here.
package exercises

import "github.com/NorthAIProject/north-client/internal/exercises/exercise"

// The exercise shape lives in a leaf package so this service and the workouts
// service that resolves plans against the catalog do not import each other.
type Exercise = exercise.Exercise

const (
	EquipmentNone  = exercise.EquipmentNone
	EquipmentOther = exercise.EquipmentOther

	DifficultyBeginner     = exercise.DifficultyBeginner
	DifficultyIntermediate = exercise.DifficultyIntermediate
	DifficultyExpert       = exercise.DifficultyExpert
)

var (
	Categories   = exercise.Categories
	Difficulties = exercise.Difficulties
)

// Filter is a browse query. Every field is optional; a zero Filter matches
// everything, capped by Limit.
type Filter struct {
	// Query matches the name, case-insensitively, anywhere in it.
	Query string

	// Muscle matches a key in either the primary or secondary list. Searching
	// both is what someone browsing means by "what trains my lats" — they want
	// the rows too, not only the ones where it is the main target.
	Muscle string

	Category string

	// Equipment matches any of the listed values. Empty means no constraint.
	Equipment []string

	Limit int
}
