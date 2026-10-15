package recaps

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/recaps/recap"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestRecapShapes(t *testing.T) {
	t.Parallel()

	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	apitest.AssertGolden(t, "recap.golden.json", projectRecap(recap.Recap{
		Key:           recap.KeyWeek,
		Since:         day(9),
		Until:         day(15),
		Totals:        recap.Totals{ActiveDays: 3, Workouts: 1, Sets: 3, FoodLogs: 2, CheckIns: 1},
		Prior:         recap.Totals{ActiveDays: 1, Workouts: 1},
		CheckInStreak: 4,
		Days: []recap.Day{
			{Date: day(9)}, {Date: day(10), Sets: 3}, {Date: day(11)}, {Date: day(12), FoodLogs: 2},
			{Date: day(13)}, {Date: day(14)}, {Date: day(15), Workouts: 1, CheckIns: 1},
		},
		Highlights: []recap.Highlight{{
			Kind: achievement.KindWorkoutCompleted, Title: "Finished Legs", Detail: "52 min",
			OccurredAt: time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC),
		}},
	}))
	apitest.AssertGolden(t, "recap-shared.golden.json", ShareView{
		ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), TrainingShared: true,
	})
}
