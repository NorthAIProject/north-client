package insights

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/insights/insightview"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
)

func TestStatsPageShapes(t *testing.T) {
	t.Parallel()

	rng := insightview.RangeView{Key: "week", Label: "Last 7 days", Options: []insightview.RangeOption{
		{Key: "week", Label: "Last 7 days", Selected: true},
	}}
	nights := viz.Props{Data: viz.Data{Labels: []string{"Oct 1", "Oct 2"}, Datasets: []viz.Dataset{{Label: "Hours", Data: []float64{7.5, 6.9}}}}}

	apitest.AssertGolden(t, "insights-sleep-page.golden.json", projectSleep(insightview.SleepView{
		Range: rng, HasData: true,
		Tiles:    []insightview.Row{{Label: "Average", Value: "7h 12m", Note: "across 2 nights"}},
		Nights:   nights,
		Stages:   []insightview.Bar{{Label: "Deep", Value: "18%", Pct: 18}},
		Schedule: []insightview.Row{{Label: "Bedtime", Value: "23:10", Note: "varies by 20m"}},
		Extremes: []insightview.Row{{Label: "Longest", Value: "7h 30m", Note: "Wed, Oct 1"}},
	}))

	apitest.AssertGolden(t, "insights-cardio-page.golden.json", projectCardio(insightview.CardioView{
		Range: rng, HasData: true,
		Tiles:       []insightview.Row{{Label: "Sessions", Value: "2"}},
		Weekly:      nights,
		WeeklyTitle: "Distance by week",
		Kinds:       []insightview.Bar{{Label: "Running (2)", Value: "1h 04m · 16.0 km", Pct: 100}},
		Runs:        []insightview.Row{{Label: "Runs", Value: "2"}},
		HasRuns:     true,
		Heart:       []insightview.Row{{Label: "Resting heart rate", Value: "55 bpm", Note: "+2 since Oct 1"}},
	}))

	apitest.AssertGolden(t, "insights-patterns-page.golden.json", projectPatterns(insightview.PatternsView{
		Range: rng, Days: 30,
		Findings: []insightview.PatternView{{
			Title: "You sleep longer after training", Detail: "Across 30 days.",
			A: insightview.Bar{Label: "Trained (8)", Value: "7h 40m", Pct: 100},
			B: insightview.Bar{Label: "Rest (22)", Value: "6h 55m", Pct: 90},
		}},
	}))

	// An empty window still answers with lists, never null.
	apitest.AssertGolden(t, "insights-sleep-page-empty.golden.json", projectSleep(insightview.SleepView{Range: rng}))
}
