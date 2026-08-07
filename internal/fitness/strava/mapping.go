package strava

import (
	"github.com/NorthAIProject/north-client/internal/activity"
)

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
var sportTypes = map[string]string{
	// Running
	"Run":        "running_9_8kmh",
	"TrailRun":   "running_8kmh", // slower over terrain, but harder per km
	"VirtualRun": "running_9_8kmh",
	"Treadmill":  "running_9_8kmh",

	// Walking and hiking
	"Walk":     "walking_moderate",
	"Hike":     "hiking",
	"Snowshoe": "hiking",

	// Cycling
	"Ride":             "cycling_moderate",
	"VirtualRide":      "cycling_moderate",
	"GravelRide":       "cycling_moderate",
	"MountainBikeRide": "cycling_vigorous",
	"EBikeRide":        "cycling_leisure",
	"Handcycle":        "cycling_leisure",
	"Velomobile":       "cycling_leisure",

	// Water
	"Swim":            "swimming_moderate",
	"Rowing":          "rowing_moderate",
	"VirtualRow":      "rowing_moderate",
	"Kayaking":        "rowing_moderate",
	"Canoeing":        "rowing_moderate",
	"StandUpPaddling": "rowing_moderate",
	"Surfing":         "swimming_leisure",
	"Kitesurf":        "swimming_moderate",
	"Windsurf":        "swimming_moderate",

	// Gym
	"WeightTraining":                "strength_training",
	"Crossfit":                      "hiit",
	"Workout":                       "hiit",
	"HighIntensityIntervalTraining": "hiit",
	"Elliptical":                    "elliptical",
	"StairStepper":                  "stair_climbing",
	"Yoga":                          "yoga",
	"Pilates":                       "pilates",

	// Snow
	"AlpineSki":      "skiing",
	"BackcountrySki": "skiing",
	"NordicSki":      "skiing",
	"Snowboard":      "skiing",
	"IceSkate":       "skiing",

	// Sport
	"Soccer":       "soccer",
	"Badminton":    "tennis",
	"Tennis":       "tennis",
	"TableTennis":  "tennis",
	"Pickleball":   "tennis",
	"Squash":       "tennis",
	"Racquetball":  "tennis",
	"Golf":         "golf",
	"RockClimbing": "climbing",
	"Skateboard":   "cycling_leisure",
	"InlineSkate":  "cycling_leisure",
}

// fallbackCode is used for a sport this table has never heard of. A moderate
// generic effort rather than skipping the activity: the session genuinely
// happened, and dropping it would leave a hole in someone's week that they
// would have to notice to report. A conservative estimate is more honest
// than silence.
const fallbackCode = "hiit"

// MapSportType returns the North activity code for a Strava sport, and
// whether the mapping was known. Callers use the second return to decide
// whether to log the gap, not whether to import.
func MapSportType(sportType, legacyType string) (string, bool) {
	if code, ok := sportTypes[sportType]; ok {
		return code, true
	}
	if code, ok := sportTypes[legacyType]; ok {
		return code, true
	}
	return fallbackCode, false
}

// ensure every code this table produces actually exists in the MET table,
// checked once at startup rather than discovered on someone's first import.
func init() {
	for sport, code := range sportTypes {
		if _, ok := activity.LookupMET(code); !ok {
			panic("strava: sport " + sport + " maps to unknown activity code " + code)
		}
	}
	if _, ok := activity.LookupMET(fallbackCode); !ok {
		panic("strava: fallback maps to unknown activity code " + fallbackCode)
	}
}
