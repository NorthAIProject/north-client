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
