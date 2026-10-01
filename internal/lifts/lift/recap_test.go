package lift_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// The recap sentence is what the person reads on the finish screen and what
// the coach is told, so it must never claim a comparison it does not have.

func squats(d int, kgs ...float64) []lift.Set {
	var out []lift.Set
	for i, kg := range kgs {
		s := set("Back squat", d, kg, 5)
		s.SetNumber = i + 1
		out = append(out, s)
	}
	return out
}

func TestRecapWithoutHistoryMakesNoComparison(t *testing.T) {
	t.Parallel()

	current := append(squats(24, 100, 100, 100), set("Bench press", 24, 60, 8))
	rc := lift.BuildRecap(42*time.Minute, 310, 5, current, nil)

	if rc.SetsDone != 4 || rc.VolumeKg != 1980 {
		t.Fatalf("sets/volume = %d/%v, want 4/1980", rc.SetsDone, rc.VolumeKg)
	}
	if want := "42 minutes, 4 of 5 sets, 1,980 kg."; rc.Sentence != want {
		t.Fatalf("sentence = %q, want %q", rc.Sentence, want)
	}
	if rc.HasComparison || rc.Exercises[0].HasPrevious {
		t.Fatal("no history means no comparison")
	}
	if len(rc.Exercises) != 2 || rc.Exercises[0].Name != "Back squat" || rc.Exercises[1].Name != "Bench press" {
		t.Fatalf("exercises in the order done, got %+v", rc.Exercises)
	}
}

func TestRecapNamesTheBiggestEstimatedMaxChange(t *testing.T) {
	t.Parallel()

	earlier := append(squats(17, 100, 100), set("Bench press", 17, 60, 8))
	current := append(squats(24, 100, 105), set("Bench press", 24, 60, 8))
	rc := lift.BuildRecap(time.Hour, 0, 0, current, earlier)

	squat := rc.Exercises[0]
	if !squat.HasPrevious || squat.Change != 5.8 {
		t.Fatalf("squat change = %v (has previous %v), want 5.8", squat.Change, squat.HasPrevious)
	}
	if squat.PreviousVolume != 1000 {
		t.Fatalf("previous volume = %v, want 1000", squat.PreviousVolume)
	}
	if want := "1 hour, 3 sets, 1,505 kg. Back squat estimated max up 5.8 kg."; rc.Sentence != want {
		t.Fatalf("sentence = %q, want %q", rc.Sentence, want)
	}
}

func TestRecapLeavesOutEmptyParts(t *testing.T) {
	t.Parallel()

	// Bodyweight only, under a minute: no volume, no duration.
	current := []lift.Set{set("Push-up", 24, 0, 20)}
	rc := lift.BuildRecap(30*time.Second, 0, 0, current, nil)
	if want := "1 set."; rc.Sentence != want {
		t.Fatalf("sentence = %q", rc.Sentence)
	}
}

func TestRecapSummaryTellsTheCoachWhichDayAndWhatChanged(t *testing.T) {
	t.Parallel()

	earlier := squats(17, 100, 100)
	rc := lift.BuildRecap(time.Hour, 0, 0, squats(24, 100, 105), earlier)
	rc.PlanWeekday, rc.Focus = "Thursday", "Legs"
	rc.StartedAt = day(24).Add(18 * time.Hour)

	got := lift.RecapSummary(rc, time.UTC)
	want := "Last workout (Thursday plan day, Legs), Thu 24 Sep: 1 hour, 2 sets, 1,025 kg. Back squat estimated max up 5.8 kg.\n" +
		"  Back squat: 2 sets, 1,025 kg volume, best 105 kg × 5 (last time 1,000 kg volume, e1RM +5.8 kg)"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
