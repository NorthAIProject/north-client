// Package preferences owns a person's standing settings: units system and
// the defaults the calculator's form pre-fills (objective, macro split).
// Dietary restrictions are not duplicated here — those live in
// internal/meals' DietPreferenceService, which already owns that data.
package preferences

import "github.com/NorthAIProject/north-client/internal/preferences/preference"

type Preferences = preference.Preferences

const (
	UnitsMetric   = preference.UnitsMetric
	UnitsImperial = preference.UnitsImperial
)

var UnitsSystems = preference.UnitsSystems
