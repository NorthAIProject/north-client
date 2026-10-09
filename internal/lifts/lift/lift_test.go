package lift_test

import (
	"strings"
	"testing"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }

func set(name string, d int, kg float64, reps int) lift.Set {
	return lift.Set{ExerciseName: name, LogDate: day(d), PerformedAt: day(d).Add(18 * time.Hour), WeightKg: kg, Reps: reps, SetNumber: 1}
}

func TestE1RM(t *testing.T) {
	t.Parallel()
	if got := lift.E1RM(100, 1); got != 100 {
		t.Errorf("single = %v, want 100", got)
	}
	if got := util.RoundHalfUpToScale(lift.E1RM(100, 5), 1); got != 116.7 {
		t.Errorf("100x5 = %v, want 116.7", got)
	}
}

func TestKeyIgnoresCaseAndSpacesButPrefersTheSlug(t *testing.T) {
	t.Parallel()
	if lift.KeyFor("", " Bench Press ") != "bench press" {
		t.Error("name key not normalised")
	}
	if lift.KeyFor("barbell-bench-press", "Bench") != "barbell-bench-press" {
		t.Error("slug not preferred")
	}
}

func TestRecordsSkipTheBaselineAndBodyweight(t *testing.T) {
	t.Parallel()
	recs := lift.Records([]lift.Set{
		set("Squat", 3, 100, 5),  // baseline
		set("Squat", 10, 100, 5), // equal: not a record
		set("Squat", 17, 105, 5), // record
		set("Pull-up", 3, 0, 10),
		set("Pull-up", 10, 0, 12),
		set("Squat", 20, 90, 3), // lighter
	})
	if len(recs) != 1 || recs[0].Set.WeightKg != 105 || recs[0].Previous != 116.7 {
		t.Fatalf("records = %+v", recs)
	}
}

func TestByExerciseGroupsAndTrends(t *testing.T) {
	t.Parallel()
	ex := lift.ByExercise([]lift.Set{
		set("Squat", 3, 100, 5), set("squat", 3, 110, 3), set("Squat", 10, 105, 5), set("Row", 3, 60, 10),
	})
	if len(ex) != 2 || ex[0].Key != "squat" || ex[0].Sets != 3 {
		t.Fatalf("exercises = %+v", ex)
	}
	if ex[0].BestWeightKg != 110 || len(ex[0].Trend) != 2 || !ex[0].Trend[0].Day.Equal(day(3)) {
		t.Errorf("squat = %+v", ex[0])
	}
	if ex[0].VolumeKg != 100*5+110*3+105*5 {
		t.Errorf("volume = %v", ex[0].VolumeKg)
	}
}

func TestWeeklyVolumeKeepsEmptyWeeks(t *testing.T) {
	t.Parallel()
	weeks := lift.WeeklyVolume([]lift.Set{set("Squat", 1, 100, 5), set("Squat", 16, 100, 5)}, day(1), day(22))
	if len(weeks) != 4 {
		t.Fatalf("weeks = %+v", weeks)
	}
	if weeks[0].Value != 500 || weeks[1].Value != 0 || weeks[2].Value != 500 {
		t.Errorf("weeks = %+v", weeks)
	}
}

func TestLastWorkoutIsTheLatestSessionInSetOrder(t *testing.T) {
	t.Parallel()
	session := uuid.New()
	a, b := set("Squat", 10, 100, 5), set("Squat", 10, 105, 5)
	a.ActivitySessionID, b.ActivitySessionID = &session, &session
	a.SetNumber, b.SetNumber = 2, 1
	older := set("Squat", 3, 90, 5)
	last := lift.LastWorkout([]lift.Set{a, older, b})
	if len(last) != 2 || last[0].SetNumber != 1 || last[1].SetNumber != 2 {
		t.Fatalf("last = %+v", last)
	}
	if lift.Workouts([]lift.Set{a, b, older}) != 2 {
		t.Error("workout count")
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()
	if !strings.Contains(lift.Summary(nil, nil), "no sets") {
		t.Error("empty summary")
	}
	sets := []lift.Set{set("Squat", 3, 100, 5), set("Squat", 17, 105, 5)}
	got := lift.Summary(sets, lift.Records(sets))
	if !strings.Contains(got, "Squat") || !strings.Contains(got, "New record") {
		t.Errorf("summary = %q", got)
	}
}

func TestWarmupsCountForNothing(t *testing.T) {
	t.Parallel()
	warm := set("Squat", 2, 60, 5)
	warm.Kind = lift.KindWarmup
	work := set("Squat", 2, 100, 5)
	work.Kind = lift.KindWork
	heavyWarm := set("Squat", 3, 140, 3) // a "record" that is only a warm-up
	heavyWarm.Kind = lift.KindWarmup
	drop := set("Squat", 3, 80, 8)
	drop.Kind = lift.KindDrop
	sets := []lift.Set{warm, work, heavyWarm, drop}

	if warm.Volume() != 0 || drop.Volume() != 640 {
		t.Errorf("volume: warm-up %v, drop %v", warm.Volume(), drop.Volume())
	}
	if recs := lift.Records(sets); len(recs) != 0 {
		t.Errorf("records = %+v, want none (the heavy set was a warm-up, the drop is lighter)", recs)
	}
	ex := lift.ByExercise(sets)
	if len(ex) != 1 || ex[0].Sets != 2 || ex[0].BestWeightKg != 100 {
		t.Errorf("by exercise = %+v, want the work and drop sets only", ex)
	}
	if got := lift.Working(sets); len(got) != 2 {
		t.Errorf("working = %d sets, want 2", len(got))
	}
	if !strings.Contains(lift.Summary(sets, nil), "2 working sets") {
		t.Errorf("summary = %q", lift.Summary(sets, nil))
	}
}

func TestValidKind(t *testing.T) {
	t.Parallel()
	for _, k := range lift.Kinds {
		if !lift.ValidKind(k) {
			t.Errorf("%q invalid", k)
		}
	}
	if lift.ValidKind("failure") || lift.ValidKind("") {
		t.Error("an unknown or empty kind is valid")
	}
}
