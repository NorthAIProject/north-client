package awards

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestAwardShapes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	b1, _ := award.ByID("workouts_1")
	b2, _ := award.ByID("streak_30")
	rar := map[string]Rarity{"workouts_1": {Earned: 1, Active: 200}}
	earned := projectEarned(ctx, Earned{Badge: b1, At: at}, rar)
	n := 12
	locked := b2.Project(ctx, nil, &n, "")
	apitest.AssertGolden(t, "awards.golden.json", AwardsView{Earned: []award.View{earned}, Locked: []award.View{locked}})
	apitest.AssertGolden(t, "unseen.golden.json", UnseenView{Awards: []award.View{earned}})
}

func TestMonthlyShapes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	b, _ := award.ByID("workouts_1")
	badge := projectEarned(ctx, Earned{Badge: b, At: at.Add(-72 * time.Hour)}, nil)
	medal := ProjectMonthlyMedal(ctx, MonthlyMedal{Key: "2026-09", Metric: MetricHabitDays, Target: 12, At: at.Add(-24 * time.Hour)})
	current := ProjectMonthlyChallenge(ctx, MonthlyChallenge{
		Key: "2026-10", Month: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Metric: MetricWorkouts, Target: 5, Progress: 3,
	})
	apitest.AssertGolden(t, "awards-monthly.golden.json", AwardsView{
		Earned:           MergeEarned([]award.View{badge}, []award.View{medal}),
		Locked:           []award.View{},
		MonthlyChallenge: &current,
	})
	apitest.AssertGolden(t, "unseen-monthly.golden.json", UnseenView{Awards: []award.View{badge, medal}})
}
