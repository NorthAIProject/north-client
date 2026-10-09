package lift_test

import (
	"math"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

func heatOf(t *testing.T, h lift.HeatMap, muscle string) lift.MuscleHeat {
	t.Helper()
	for _, m := range h.Muscles {
		if m.Muscle == muscle {
			return m
		}
	}
	t.Fatalf("no heat for %q in %+v", muscle, h.Muscles)
	return lift.MuscleHeat{}
}

func TestHeatDecayMatchesTheWeekAnchors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		age  int
		want float64
	}{{0, 1}, {1, 1}, {3, 0.6}, {7, 0.2}} {
		if got := lift.HeatDecay(tc.age); math.Abs(got-tc.want) > 0.025 {
			t.Errorf("HeatDecay(%d) = %.3f, want about %.1f", tc.age, got, tc.want)
		}
	}
}

func TestHeatCountsPrimaryFullyAndSecondaryAtPointFour(t *testing.T) {
	t.Parallel()
	h := lift.HeatOf(setsAgo("barbell-squat", 1, 24*time.Hour), weights, loadNow, time.UTC, 7)

	quads := heatOf(t, h, "quads").Intensity
	hamstrings := heatOf(t, h, "hamstrings").Intensity
	if want := 1 - math.Exp(-1.0/4); math.Abs(quads-want) > 1e-9 {
		t.Errorf("quads = %.4f, want %.4f", quads, want)
	}
	if want := 1 - math.Exp(-lift.SecondaryWeight/4); math.Abs(hamstrings-want) > 1e-9 {
		t.Errorf("hamstrings = %.4f, want %.4f", hamstrings, want)
	}
}

func TestHeatAgesByCalendarDayWhereThePersonIs(t *testing.T) {
	t.Parallel()
	lisbon, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skip("no tzdata")
	}
	// 01:30 in Lisbon on the 8th; a set at 23:30 on the 5th is three days ago
	// there, although it is only 50 hours earlier.
	now := time.Date(2026, 10, 8, 1, 30, 0, 0, lisbon)
	at := time.Date(2026, 10, 5, 23, 30, 0, 0, lisbon)
	sets := []lift.Set{{ExerciseSlug: "barbell-squat", PerformedAt: at}}

	got := heatOf(t, lift.HeatOf(sets, weights, now, lisbon, 7), "quads").Intensity
	if want := 1 - math.Exp(-lift.HeatDecay(3)/4); math.Abs(got-want) > 1e-9 {
		t.Errorf("quads = %.4f, want three days' decay %.4f", got, want)
	}
}

func TestHeatOutsideTheWindowKeepsTheDateButNoIntensity(t *testing.T) {
	t.Parallel()
	h := lift.HeatOf(setsAgo("bench-press", 3, 20*24*time.Hour), weights, loadNow, time.UTC, 7)

	chest := heatOf(t, h, "chest")
	if chest.Intensity != 0 {
		t.Errorf("intensity = %.3f, want 0 outside the window", chest.Intensity)
	}
	if chest.LastTrained.IsZero() || h.LastSession.IsZero() {
		t.Errorf("dates lost: %+v, last session %v", chest, h.LastSession)
	}
}

func TestHeatIgnoresWarmupsFutureSetsAndUnknownExercises(t *testing.T) {
	t.Parallel()
	warmup := setsAgo("barbell-squat", 2, time.Hour)
	for i := range warmup {
		warmup[i].Kind = lift.KindWarmup
	}
	sets := append(warmup, setsAgo("barbell-squat", 1, -time.Hour)...)
	sets = append(sets, setsAgo("typed-in-lunge", 4, time.Hour)...)

	h := lift.HeatOf(sets, weights, loadNow, time.UTC, 7)
	if len(h.Muscles) != 0 {
		t.Errorf("muscles = %+v, want none", h.Muscles)
	}
}
