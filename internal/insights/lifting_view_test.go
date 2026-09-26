package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func TestLiftingViewIsEmptyWithoutSets(t *testing.T) {
	t.Parallel()
	if v := buildLiftingView(lifts.Stats{}); v.HasSets {
		t.Errorf("no sets but HasSets: %+v", v)
	}
}

func TestLiftingViewAddsUp(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	set := func(d int, kg float64, reps int) lift.Set {
		return lift.Set{ExerciseName: "Squat", LogDate: day(d), PerformedAt: day(d).Add(18 * time.Hour), WeightKg: kg, Reps: reps, SetNumber: 1}
	}
	sets := []lift.Set{set(17, 105, 5), set(3, 100, 5)}
	rg := timerange.Parse(timerange.KeyMonth, time.UTC)
	rg.Since, rg.Until = day(1), day(29)
	v := buildLiftingView(lifts.Stats{
		Range: rg, Sets: sets, Exercises: lift.ByExercise(sets), Records: lift.Records(sets),
		Weekly: lift.WeeklyVolume(sets, rg.Since, rg.Until), Muscles: []lifts.MuscleSets{{Muscle: "lower_back", Sets: 2}, {Muscle: "quads", Sets: 1}},
		PriorVolumeKg: 500,
	})
	if !v.HasSets || v.Volume != "1025 kg" || v.Workouts != 2 || !v.Delta.HasPrior {
		t.Errorf("tiles = %+v", v)
	}
	if len(v.Exercises) != 1 || v.Exercises[0].Change != 5.8 {
		t.Errorf("exercises = %+v", v.Exercises)
	}
	if len(v.Records) != 1 || v.Records[0].Lift != "105 kg × 5" || v.Records[0].Gain != "5.8 kg" {
		t.Errorf("records = %+v", v.Records)
	}
	if v.Muscles[0].Name != "Lower back" || v.Muscles[1].Pct != 50 {
		t.Errorf("muscles = %+v", v.Muscles)
	}
}

func TestTrainingPanelsRenderLifting(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	sets := []lift.Set{
		{ExerciseName: "Squat", LogDate: day, PerformedAt: day, WeightKg: 100, Reps: 5, SetNumber: 1},
		{ExerciseName: "Squat", LogDate: day.AddDate(0, 0, 7), PerformedAt: day.AddDate(0, 0, 7), WeightKg: 110, Reps: 5, SetNumber: 1},
	}
	view, err := buildTrainingView(TrainingData{
		Range: timerange.Parse(timerange.KeyMonth, time.UTC),
		Lifts: lifts.Stats{Sets: sets, Exercises: lift.ByExercise(sets), Records: lift.Records(sets)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := insightpages.TrainingPanels(view).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, want := range []string{`data-testid="insights-lifting"`, `data-testid="lifting-records"`, "Squat", "110 kg × 5"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered training panels lack %q", want)
		}
	}
}
