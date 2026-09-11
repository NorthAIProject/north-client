package strava

import (
	"sort"

	"github.com/NorthAIProject/north-client/internal/activity"
)

// SportFamily is the coarse grouping the interface colours and legends by.
//
// Coarser than the MET codes deliberately: "running_8kmh" and
// "running_9_8kmh" are two calorie estimates and one colour. Six families is
// what a real week contains — the first version of the activity scene kept
// only run, ride and swim distinct, and on an actual account, dominated by
// walks and gym sessions, a field where every tile is the same grey told you
// nothing.
//
// This lives beside the MET mapping rather than in the viewer because it was
// in the viewer, in JavaScript, and the two tables could already disagree
// about what a Kitesurf is. One table, one place, one answer on the wire.
type SportFamily string

const (
	FamilyRun      SportFamily = "run"
	FamilyRide     SportFamily = "ride"
	FamilySwim     SportFamily = "swim"
	FamilyWalk     SportFamily = "walk"
	FamilyStrength SportFamily = "strength"
	FamilyOther    SportFamily = "other"
)

// Families is every family, in the order the legend shows them. Ordering is
// part of the contract: a tie between two families resolves by this order, so
// the same day never colours itself differently on two page loads.
var Families = []SportFamily{
	FamilyRun,
	FamilyRide,
	FamilySwim,
	FamilyWalk,
	FamilyStrength,
	FamilyOther,
}

// Label is the family as a legend shows it.
func (f SportFamily) Label() string {
	switch f {
	case FamilyRun:
		return "Run"
	case FamilyRide:
		return "Ride"
	case FamilySwim:
		return "Swim"
	case FamilyWalk:
		return "Walk"
	case FamilyStrength:
		return "Strength"
	default:
		return "Other"
	}
}

// sportMapping is what one Strava sport resolves to. Code and Family travel
// together so a new sport cannot be given a calorie estimate and quietly left
// without a colour.
type sportMapping struct {
	Code   string
	Family SportFamily
}

// sportTypes maps Strava's sport_type values onto North's own MET codes
// (internal/activity/activity.METTable).
//
// Deliberately a translation table rather than a shared vocabulary: Strava's
// list is Strava's to change, and North's codes are tuned for calorie
// estimation from MET values. Keeping them separate means a new Strava sport
// is one line here rather than a change to the table every manual session
// also validates against.
//
// Strava sends both `type` (legacy, coarse) and `sport_type` (newer, finer).
// This table is keyed on sport_type, and the client falls back to type.
var sportTypes = map[string]sportMapping{
	// Running
	"Run":        {"running_9_8kmh", FamilyRun},
	"TrailRun":   {"running_8kmh", FamilyRun}, // slower over terrain, but harder per km
	"VirtualRun": {"running_9_8kmh", FamilyRun},
	"Treadmill":  {"running_9_8kmh", FamilyRun},

	// Walking and hiking
	"Walk":     {"walking_moderate", FamilyWalk},
	"Hike":     {"hiking", FamilyWalk},
	"Snowshoe": {"hiking", FamilyWalk},

	// Cycling
	"Ride":             {"cycling_moderate", FamilyRide},
	"VirtualRide":      {"cycling_moderate", FamilyRide},
	"GravelRide":       {"cycling_moderate", FamilyRide},
	"MountainBikeRide": {"cycling_vigorous", FamilyRide},
	"EBikeRide":        {"cycling_leisure", FamilyRide},
	"Handcycle":        {"cycling_leisure", FamilyRide},
	"Velomobile":       {"cycling_leisure", FamilyRide},

	// Water
	"Swim":            {"swimming_moderate", FamilySwim},
	"Rowing":          {"rowing_moderate", FamilyOther},
	"VirtualRow":      {"rowing_moderate", FamilyOther},
	"Kayaking":        {"rowing_moderate", FamilyOther},
	"Canoeing":        {"rowing_moderate", FamilyOther},
	"StandUpPaddling": {"rowing_moderate", FamilyOther},
	"Surfing":         {"swimming_leisure", FamilyOther},
	"Kitesurf":        {"swimming_moderate", FamilyOther},
	"Windsurf":        {"swimming_moderate", FamilyOther},

	// Gym
	"WeightTraining":                {"strength_training", FamilyStrength},
	"Crossfit":                      {"hiit", FamilyStrength},
	"Workout":                       {"hiit", FamilyStrength},
	"HighIntensityIntervalTraining": {"hiit", FamilyStrength},
	"Elliptical":                    {"elliptical", FamilyOther},
	"StairStepper":                  {"stair_climbing", FamilyOther},
	"Yoga":                          {"yoga", FamilyOther},
	"Pilates":                       {"pilates", FamilyOther},

	// Snow
	"AlpineSki":      {"skiing", FamilyOther},
	"BackcountrySki": {"skiing", FamilyOther},
	"NordicSki":      {"skiing", FamilyOther},
	"Snowboard":      {"skiing", FamilyOther},
	"IceSkate":       {"skiing", FamilyOther},

	// Sport
	"Soccer":       {"soccer", FamilyOther},
	"Badminton":    {"tennis", FamilyOther},
	"Tennis":       {"tennis", FamilyOther},
	"TableTennis":  {"tennis", FamilyOther},
	"Pickleball":   {"tennis", FamilyOther},
	"Squash":       {"tennis", FamilyOther},
	"Racquetball":  {"tennis", FamilyOther},
	"Golf":         {"golf", FamilyOther},
	"RockClimbing": {"climbing", FamilyOther},
	"Skateboard":   {"cycling_leisure", FamilyOther},
	"InlineSkate":  {"cycling_leisure", FamilyOther},
}

// fallbackCode is used for a sport this table has never heard of. A moderate
// generic effort rather than skipping the activity: the session genuinely
// happened, and dropping it would leave a hole in someone's week that they
// would have to notice to report. A conservative estimate is more honest
// than silence.
const fallbackCode = "hiit"

// fallbackFamily is where an unmapped sport is drawn. Neutral rather than
// guessed: colouring an unknown sport as a run would be a confident lie, and
// the neutral swatch is exactly what "we do not know what this was" looks
// like on a legend.
const fallbackFamily = FamilyOther

// MapSportType returns the North activity code for a Strava sport, and
// whether the mapping was known. Callers use the second return to decide
// whether to log the gap, not whether to import.
func MapSportType(sportType, legacyType string) (string, bool) {
	if m, ok := lookup(sportType, legacyType); ok {
		return m.Code, true
	}
	return fallbackCode, false
}

// Family returns the colour grouping for a Strava sport.
//
// Unlike MapSportType there is no "was this known" return: an unrecognised
// sport is genuinely FamilyOther, which is a correct answer rather than a
// fallback, and a caller that wants to log the gap already has MapSportType.
func Family(sportType, legacyType string) SportFamily {
	if m, ok := lookup(sportType, legacyType); ok {
		return m.Family
	}
	return fallbackFamily
}

// AllSportTypes is every Strava sport this table knows, sorted so callers
// that render or assert over it get a stable order.
//
// Exported for the tests, which need to walk the table to check that every
// family is reachable from at least one sport — a direction the startup panic
// cannot check, and the one that catches a swatch standing for nothing.
func AllSportTypes() []string {
	out := make([]string, 0, len(sportTypes))
	for sport := range sportTypes {
		out = append(out, sport)
	}
	sort.Strings(out)
	return out
}

// lookup prefers sport_type and falls back to the legacy type, so both
// callers agree on which of the two won.
func lookup(sportType, legacyType string) (sportMapping, bool) {
	if m, ok := sportTypes[sportType]; ok {
		return m, true
	}
	m, ok := sportTypes[legacyType]
	return m, ok
}

// ensure every code this table produces actually exists in the MET table,
// checked once at startup rather than discovered on someone's first import.
func init() {
	known := make(map[SportFamily]bool, len(Families))
	for _, f := range Families {
		known[f] = true
	}

	for sport, m := range sportTypes {
		if _, ok := activity.LookupMET(m.Code); !ok {
			panic("strava: sport " + sport + " maps to unknown activity code " + m.Code)
		}
		if !known[m.Family] {
			panic("strava: sport " + sport + " maps to unknown family " + string(m.Family))
		}
	}
	if _, ok := activity.LookupMET(fallbackCode); !ok {
		panic("strava: fallback maps to unknown activity code " + fallbackCode)
	}
	if !known[fallbackFamily] {
		panic("strava: fallback maps to unknown family " + string(fallbackFamily))
	}
}
