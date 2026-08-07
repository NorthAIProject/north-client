// Package preference holds the shape of a person's standing settings: units
// system, and the defaults the calculator's form should pre-fill.
//
// A leaf, so the preferences service and any future template that renders it
// do not import each other. See CLAUDE.md on slice layout.
package preference

import (
	"time"

	"github.com/google/uuid"
)

// Units systems a person can work in.
const (
	UnitsMetric   = "metric"
	UnitsImperial = "imperial"
)

// UnitsSystems is the ordered set offered in the UI.
var UnitsSystems = []string{UnitsMetric, UnitsImperial}

// Preferences is a person's standing settings. DefaultGoal/DefaultMacroSplit
// use the same string values as calculator.Goal/calculator.MacroSplit — kept
// as plain strings here rather than importing calculator, so this leaf stays
// free of other feature packages; the service layer validates against
// calculator's exported enums instead.
type Preferences struct {
	UserID uuid.UUID

	UnitsSystem       string
	DefaultGoal       string
	DefaultMacroSplit string

	UpdatedAt time.Time
}

// Summary renders preferences for the coach's context.
func (p Preferences) Summary() string {
	return "Units: " + p.UnitsSystem + ". Default objective: " + p.DefaultGoal + ". Default macro split: " + p.DefaultMacroSplit + "."
}
