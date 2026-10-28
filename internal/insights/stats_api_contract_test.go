package insights

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

func TestStatsPageShapes(t *testing.T) {
	t.Parallel()

	rng := insightpages.RangeView{Key: "week", Label: "Last 7 days", Options: []insightpages.RangeOption{
		{Key: "week", Label: "Last 7 days", Selected: true},
	}}
	nights := chart.Props{Data: chart.Data{Labels: []string{"Oct 1", "Oct 2"}, Datasets: []chart.Dataset{{Label: "Hours", Data: []float64{7.5, 6.9}}}}}

	apitest.AssertGolden(t, "insights-sleep-page.golden.json", projectSleep(insightpages.SleepView{
		Range: rng, HasData: true,
		Tiles:    []insightpages.Row{{Label: "Average", Value: "7h 12m", Note: "across 2 nights"}},
		Nights:   nights,
		Stages:   []insightpages.Bar{{Label: "Deep", Value: "18%", Pct: 18}},
		Schedule: []insightpages.Row{{Label: "Bedtime", Value: "23:10", Note: "varies by 20m"}},
		Extremes: []insightpages.Row{{Label: "Longest", Value: "7h 30m", Note: "Wed, Oct 1"}},
	}))

	apitest.AssertGolden(t, "insights-cardio-page.golden.json", projectCardio(insightpages.CardioView{
		Range: rng, HasData: true,
		Tiles:       []insightpages.Row{{Label: "Sessions", Value: "2"}},
		Weekly:      nights,
		WeeklyTitle: "Distance by week",
		Kinds:       []insightpages.Bar{{Label: "Running (2)", Value: "1h 04m · 16.0 km", Pct: 100}},
		Runs:        []insightpages.Row{{Label: "Runs", Value: "2"}},
		HasRuns:     true,
		Heart:       []insightpages.Row{{Label: "Resting heart rate", Value: "55 bpm", Note: "+2 since Oct 1"}},
	}))

	apitest.AssertGolden(t, "insights-patterns-page.golden.json", projectPatterns(insightpages.PatternsView{
		Range: rng, Days: 30,
		Findings: []insightpages.PatternView{{
			Title: "You sleep longer after training", Detail: "Across 30 days.",
			A: insightpages.Bar{Label: "Trained (8)", Value: "7h 40m", Pct: 100},
			B: insightpages.Bar{Label: "Rest (22)", Value: "6h 55m", Pct: 90},
		}},
	}))

	// An empty window still answers with lists, never null.
	apitest.AssertGolden(t, "insights-sleep-page-empty.golden.json", projectSleep(insightpages.SleepView{Range: rng}))
}
