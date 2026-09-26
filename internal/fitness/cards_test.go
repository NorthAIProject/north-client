package fitness

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
)

func TestStrengthAndCardioCardsAreLeftOutWhenEmpty(t *testing.T) {
	t.Parallel()
	if strengthView(nil) != nil || strengthView(&lifts.Stats{}) != nil {
		t.Error("an empty month drew a strength card")
	}
	if cardioView(nil) != nil || cardioView(&stats.CardioStats{}) != nil {
		t.Error("an empty month drew a cardio card")
	}
}

func TestStrengthCardSummarisesTheMonth(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	sets := []lift.Set{
		{ExerciseName: "Squat", LogDate: day(3), PerformedAt: day(3), WeightKg: 100, Reps: 5, SetNumber: 1},
		{ExerciseName: "Squat", LogDate: day(17), PerformedAt: day(17), WeightKg: 105, Reps: 5, SetNumber: 1},
	}
	v := strengthView(&lifts.Stats{Sets: sets, Exercises: lift.ByExercise(sets), Records: lift.Records(sets)})
	if v == nil || v.Workouts != 2 || v.Sets != 2 || v.VolumeKg != 1025 {
		t.Fatalf("strength = %+v", v)
	}
	if v.Exercises[0].ChangeKg != 5.8 || v.Record == nil || v.Record.WeightKg != 105 {
		t.Errorf("rows %+v record %+v", v.Exercises, v.Record)
	}

	c := cardioView(&stats.CardioStats{
		CardioStats: stat.Cardio([]stat.Session{{Code: "running_11_3kmh", Name: "Running", At: day(2), Seconds: 1500, DistanceM: 5000}}, day(1), day(30)),
		RestingHR:   []stat.DayValue{{Day: day(2), Value: 52}},
	})
	if c == nil || c.Runs != 1 || c.Best5KSeconds != 1500 || c.RestingHR != 52 {
		t.Errorf("cardio = %+v", c)
	}
}
