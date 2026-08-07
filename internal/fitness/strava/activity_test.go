package strava_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
)

func TestPaceIsMinutesPerKilometre(t *testing.T) {
	t.Parallel()

	// 10km in 50 minutes is 5:00/km.
	a := strava.Activity{DistanceM: 10000, MovingTimeS: 50 * 60}
	if got := a.PaceMinPerKm(); got != 5 {
		t.Errorf("PaceMinPerKm = %v, want 5", got)
	}
}

// A gym session has no distance, so pace is not a question with an answer.
// Zero is the signal for that, and the template checks it before rendering.
func TestPaceOfADistancelessActivityIsZero(t *testing.T) {
	t.Parallel()

	for _, a := range []strava.Activity{
		{DistanceM: 0, MovingTimeS: 3600},
		{DistanceM: 5000, MovingTimeS: 0},
	} {
		if got := a.PaceMinPerKm(); got != 0 {
			t.Errorf("PaceMinPerKm(%+v) = %v, want 0", a, got)
		}
	}
}

func TestHasRouteDistinguishesIndoorFromRecorded(t *testing.T) {
	t.Parallel()

	outdoor := strava.Activity{SummaryPolyline: "_p~iF~ps|U"}
	if !outdoor.HasRoute() {
		t.Error("an activity with a polyline reported no route")
	}

	treadmill := strava.Activity{SummaryPolyline: ""}
	if treadmill.HasRoute() {
		t.Error("an activity without a polyline reported a route")
	}
}

func TestDistanceKm(t *testing.T) {
	t.Parallel()

	a := strava.Activity{DistanceM: 5432, StartDate: time.Now()}
	if got := a.DistanceKm(); got != 5.432 {
		t.Errorf("DistanceKm = %v, want 5.432", got)
	}
}
