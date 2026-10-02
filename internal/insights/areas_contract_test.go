package insights

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/insights/score"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestAreasShape(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	data := Areas{Weeks: []time.Time{monday.AddDate(0, 0, -7), monday}}
	for _, d := range domains {
		now := score.Score{Domain: d.Key}
		trend := []AreaPoint{{}, {}}
		if d.Key == "body" {
			now = score.New("body", []score.Component{{Key: "sleep_duration", Earned: 25, Weight: 40, Known: true}, {Key: "hydration", Earned: 30, Weight: 30, Known: true}})
			trend = []AreaPoint{{Points: 64, HasData: true}, {Points: now.Points, HasData: true}}
		}
		data.Areas = append(data.Areas, Area{Key: d.Key, Label: d.Label, Now: now, Trend: trend})
	}
	view, err := projectAreas(data, []string{"Priorities: Three runs", "Training: deload week, about 60% of the plan's sets"})
	if err != nil {
		t.Fatal(err)
	}
	apitest.AssertGolden(t, "insights-areas.golden.json", view)
}
