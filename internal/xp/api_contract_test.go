package xp

import (
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestXPShapes(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "summary.golden.json", projectSummary(Summary{
		Week: []Earned{
			{Kind: KindWorkout, Count: 2, Points: 40},
			{Kind: KindHabitKept, Count: 0, Points: 0},
			{Kind: KindWeekReviewed, Count: 1, Points: 25},
		},
		WeekTotal: 65, Total: 340, Level: LevelFor(340),
	}))
	top := LevelFor(7000)
	apitest.AssertGolden(t, "board.golden.json", projectBoard(Board{
		Metric: MetricXP, Period: PeriodWeek, Sharing: true,
		Entries: []Entry{
			{Rank: 1, UserID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), DisplayName: "Bo", Handle: "bo_lifts", Value: 120, Level: &top},
			{Rank: 2, UserID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), DisplayName: "Ana", Value: 40, Me: true},
		},
	}))
}
