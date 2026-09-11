package strava_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
)

func TestKnownSportsMapToRealActivityCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sportType string
		want      string
	}{
		{"Run", "running_9_8kmh"},
		{"TrailRun", "running_8kmh"},
		{"Ride", "cycling_moderate"},
		{"MountainBikeRide", "cycling_vigorous"},
		{"Swim", "swimming_moderate"},
		{"WeightTraining", "strength_training"},
		{"Yoga", "yoga"},
		{"Walk", "walking_moderate"},
		{"Hike", "hiking"},
		{"Rowing", "rowing_moderate"},
	}

	for _, tt := range tests {
		t.Run(tt.sportType, func(t *testing.T) {
			t.Parallel()

			code, known := strava.MapSportType(tt.sportType, "")
			if !known {
				t.Fatalf("MapSportType(%q) reported unknown", tt.sportType)
			}
			if code != tt.want {
				t.Errorf("MapSportType(%q) = %q, want %q", tt.sportType, code, tt.want)
			}
			if _, ok := activity.LookupMET(code); !ok {
				t.Errorf("%q is not a real MET code", code)
			}
		})
	}
}

// Strava sends both a legacy `type` and a newer `sport_type`. Older payloads
// and some endpoints only carry the former, so it has to be a real fallback
// rather than decoration.
func TestLegacyTypeIsUsedWhenSportTypeIsUnknown(t *testing.T) {
	t.Parallel()

	code, known := strava.MapSportType("", "Ride")
	if !known {
		t.Fatal("legacy type was not consulted")
	}
	if code != "cycling_moderate" {
		t.Errorf("code = %q, want cycling_moderate", code)
	}
}

// An unmapped sport still imports: the session genuinely happened, and
// dropping it would leave a hole someone has to notice to report. The bool
// is how the gap gets logged instead.
func TestAnUnknownSportFallsBackRatherThanFailing(t *testing.T) {
	t.Parallel()

	code, known := strava.MapSportType("Quidditch", "Quidditch")
	if known {
		t.Error("an invented sport reported as known")
	}
	if code == "" {
		t.Fatal("no fallback code returned")
	}
	if _, ok := activity.LookupMET(code); !ok {
		t.Errorf("fallback %q is not a real MET code", code)
	}
}

// The family is what the interface colours by, and it used to live in
// JavaScript as a set of substring tests on the sport name. These are the
// cases that classifier got wrong: "Treadmill" does not contain "run",
// "Snowshoe" does not contain "walk" or "hike", and "Velomobile" contains
// none of "ride", "cycl" or "bike", so all three fell through to grey.
func TestFamilyForSportsTheSubstringMatcherMissed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sportType string
		want      strava.SportFamily
	}{
		{"Treadmill", strava.FamilyRun},
		{"Snowshoe", strava.FamilyWalk},
		{"Velomobile", strava.FamilyRide},
	}

	for _, tt := range tests {
		t.Run(tt.sportType, func(t *testing.T) {
			t.Parallel()

			if got := strava.Family(tt.sportType, ""); got != tt.want {
				t.Errorf("Family(%q) = %q, want %q", tt.sportType, got, tt.want)
			}
		})
	}
}

// Gym cardio and mat work keep the neutral colour they already had. Folding
// them into strength would make that bucket look fuller than someone's week
// actually was, which is the opposite of what the colour is for.
func TestGymCardioIsNotStrength(t *testing.T) {
	t.Parallel()

	for _, sport := range []string{"Elliptical", "StairStepper", "Yoga", "Pilates"} {
		if got := strava.Family(sport, ""); got == strava.FamilyStrength {
			t.Errorf("Family(%q) = strength; gym cardio should stay neutral", sport)
		}
	}
}

func TestFamilyFallsBackToOther(t *testing.T) {
	t.Parallel()

	if got := strava.Family("Quidditch", "Quidditch"); got != strava.FamilyOther {
		t.Errorf("Family of an invented sport = %q, want other", got)
	}
}

func TestFamilyPrefersSportTypeOverLegacyType(t *testing.T) {
	t.Parallel()

	if got := strava.Family("Swim", "Ride"); got != strava.FamilySwim {
		t.Errorf("Family = %q, want swim: sport_type should win over the legacy type", got)
	}
	if got := strava.Family("", "Ride"); got != strava.FamilyRide {
		t.Errorf("Family = %q, want ride: the legacy type is a real fallback", got)
	}
}

// The startup panic already asserted that every sport maps to a real MET code
// and a known family. A test says so out loud, and adds the direction the
// panic cannot check: a family nothing maps to would exist only in the
// legend, which is how a swatch comes to stand for nothing.
func TestEveryFamilyIsReachable(t *testing.T) {
	t.Parallel()

	reached := make(map[strava.SportFamily]bool, len(strava.Families))
	for _, sport := range strava.AllSportTypes() {
		reached[strava.Family(sport, "")] = true
	}
	// Nothing maps to the fallback explicitly; an unknown sport reaches it.
	reached[strava.Family("Quidditch", "")] = true

	for _, f := range strava.Families {
		if !reached[f] {
			t.Errorf("family %q is in Families but no sport type produces it", f)
		}
	}
}

func TestFamilyLabelsAreDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[string]strava.SportFamily, len(strava.Families))
	for _, f := range strava.Families {
		label := f.Label()
		if label == "" {
			t.Errorf("family %q has no label", f)
		}
		if prev, dup := seen[label]; dup {
			t.Errorf("families %q and %q share the label %q", prev, f, label)
		}
		seen[label] = f
	}
}
