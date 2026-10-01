package lifts_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

func fixtureSets() []lifts.Set {
	session := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	return []lifts.Set{
		{
			ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), ActivitySessionID: &session, LogDate: day,
			ExerciseSlug: "barbell-squat", ExerciseName: "Barbell squat", SetNumber: 1, WeightKg: 100, Reps: 5,
			PerformedAt: day.Add(18 * time.Hour),
		},
		{
			ID: uuid.MustParse("11111111-1111-1111-1111-111111111112"), ActivitySessionID: &session, LogDate: day,
			ExerciseSlug: "barbell-squat", ExerciseName: "Barbell squat", SetNumber: 2, WeightKg: 105, Reps: 5,
			PerformedAt: day.Add(18*time.Hour + 3*time.Minute),
		},
	}
}

func TestLiftLastShape(t *testing.T) {
	t.Parallel()
	sets := fixtureSets()
	apitest.AssertGolden(t, "lift_last.golden.json", lifts.ProjectLast([]string{"barbell-squat", "deadlift"},
		map[string][]lifts.Set{"barbell-squat": sets}))
}

func TestLiftStatsShape(t *testing.T) {
	t.Parallel()
	sets := fixtureSets()
	rg := timerange.Parse(timerange.KeyWeek, time.UTC)
	rg.Since = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	rg.Until = time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	apitest.AssertGolden(t, "lift_stats.golden.json", lifts.ProjectStats(lifts.Stats{
		Range:         rg,
		Sets:          sets,
		Exercises:     lift.ByExercise(sets),
		Records:       lift.Records(sets),
		Weekly:        lift.WeeklyVolume(sets, rg.Since, rg.Until),
		Muscles:       []lifts.MuscleSets{{Muscle: "quads", Sets: 2}},
		PriorVolumeKg: 900,
	}))
}

func TestLiftRecapShape(t *testing.T) {
	t.Parallel()
	earlier := fixtureSets()
	for i := range earlier {
		earlier[i].PerformedAt = earlier[i].PerformedAt.AddDate(0, 0, -7)
		earlier[i].LogDate = earlier[i].LogDate.AddDate(0, 0, -7)
		earlier[i].WeightKg -= 5
	}
	rc := lift.BuildRecap(42*time.Minute, 310.4, 6, fixtureSets(), earlier)
	rc.SessionID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	rc.StartedAt = time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC)
	rc.PlanWeekday, rc.Focus = "Thursday", "Lower body"
	apitest.AssertGolden(t, "lift_recap.golden.json", lifts.ProjectRecap(rc))
}
