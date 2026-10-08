package lift_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

var (
	loadNow = time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	weights = lift.Weights{
		"barbell-squat": lift.MuscleWeights([]string{"quads", "glutes"}, []string{"hamstrings"}),
		"bench-press":   lift.MuscleWeights([]string{"chest"}, []string{"triceps", "delts"}),
	}
)

func setsAgo(slug string, n int, ago time.Duration) []lift.Set {
	out := make([]lift.Set, n)
	for i := range out {
		at := loadNow.Add(-ago)
		out[i] = lift.Set{ExerciseSlug: slug, ExerciseName: slug, LogDate: at, PerformedAt: at, SetNumber: i + 1, WeightKg: 60, Reps: 8}
	}
	return out
}

func TestMuscleWeightsPrimaryWinsOverSecondary(t *testing.T) {
	t.Parallel()
	w := lift.MuscleWeights([]string{"chest"}, []string{"triceps", "chest"})
	if w["chest"] != 1 || w["triceps"] != lift.SecondaryWeight {
		t.Errorf("weights = %v", w)
	}
}

func TestFatigueHalvesEveryHalfLife(t *testing.T) {
	t.Parallel()
	fresh := lift.Fatigue(setsAgo("barbell-squat", 1, 0), weights, loadNow)["quads"]
	later := lift.Fatigue(setsAgo("barbell-squat", 1, lift.FatigueHalfLife), weights, loadNow)["quads"]
	// One set is far below the curve's knee, so fatigue is near-linear in it.
	if ratio := later / fresh; math.Abs(ratio-0.5) > 0.03 {
		t.Errorf("after one half-life = %.3f of fresh, want about 0.5", ratio)
	}
}

func TestFatigueGrowsWithEverySetAndStaysBelowOne(t *testing.T) {
	t.Parallel()
	prev := 0.0
	for n := 1; n <= 40; n++ {
		got := lift.Fatigue(setsAgo("barbell-squat", n, 2*time.Hour), weights, loadNow)["quads"]
		if got <= prev || got >= 1 {
			t.Fatalf("%d sets = %.4f after %.4f, want rising and below 1", n, got, prev)
		}
		prev = got
	}
}

func TestFatigueIgnoresFutureAndUnknownExercises(t *testing.T) {
	t.Parallel()
	sets := append(setsAgo("barbell-squat", 3, -time.Hour), setsAgo("", 5, time.Hour)...)
	if got := lift.Fatigue(sets, weights, loadNow); len(got) != 0 {
		t.Errorf("fatigue = %v, want none", got)
	}
}

func TestStateOf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fatigue float64
		want    lift.State
	}{{0, lift.StateReady}, {0.24, lift.StateReady}, {0.25, lift.StateRecovering}, {0.5, lift.StateRecovering}, {0.51, lift.StateFatigued}} {
		if got := lift.StateOf(tc.fatigue); got != tc.want {
			t.Errorf("StateOf(%v) = %s, want %s", tc.fatigue, got, tc.want)
		}
	}
}

func TestStrengthHoldsThenFadesToAFloor(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	for _, tc := range []struct {
		gap  time.Duration
		want float64
	}{{0, 1}, {14 * day, 1}, {42 * day, 0.5}, {28 * day, math.Pow(0.5, 0.5)}, {400 * day, 0.5}} {
		if got := lift.Strength(loadNow.Add(-tc.gap), loadNow); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("after %v = %.4f, want %.4f", tc.gap, got, tc.want)
		}
	}
}

func TestLoadSkippedLegsAfterAHeavyPushDay(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	sets := append(setsAgo("bench-press", 8, 3*time.Hour), setsAgo("barbell-squat", 5, 25*day)...)
	sets = append(sets, setsAgo("", 2, day)...) // typed in, no catalog muscles
	load := lift.LoadOf(sets, weights, loadNow)

	if fatigued := load.In(lift.StateFatigued); len(fatigued) != 1 || fatigued[0].Muscle != "chest" {
		t.Errorf("fatigued = %+v, want chest only", fatigued)
	}
	var detrained []string
	for _, m := range load.Detrained() {
		detrained = append(detrained, m.Muscle)
	}
	if strings.Join(detrained, ",") != "glutes,hamstrings,quads" {
		t.Errorf("detrained = %v, want the squat's muscles", detrained)
	}
	if load.Unmapped != 2 {
		t.Errorf("unmapped = %d, want 2", load.Unmapped)
	}

	got := lift.ReadinessSummary(load)
	for _, want := range []string{
		"last lifting session today (Thu Oct 8)",
		"Fatigued, rest these: chest.",
		"quads (25 days)",
		"2 recent sets are exercises outside the catalog",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestReadinessSummaryCountsCalendarDays(t *testing.T) {
	t.Parallel()
	// Twenty hours ago is 22:00 the night before: under a day, still yesterday.
	load := lift.LoadOf(setsAgo("bench-press", 1, 20*time.Hour), weights, loadNow)
	if got := lift.ReadinessSummary(load); !strings.Contains(got, "yesterday") {
		t.Errorf("summary = %q, want yesterday", got)
	}
	if got := lift.ReadinessSummary(lift.LoadOf(nil, weights, loadNow)); got != "Training readiness: no sets logged yet." {
		t.Errorf("empty = %q", got)
	}
}
