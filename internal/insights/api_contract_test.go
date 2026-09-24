package insights

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

func TestInsightsShapes(t *testing.T) {
	t.Parallel()

	rng := insightpages.RangeView{Key: "week", Label: "Last 7 days", Options: []insightpages.RangeOption{
		{Key: "week", Label: "Last 7 days", Selected: true}, {Key: "month", Label: "Last 30 days"},
	}}
	week := chart.Data{Labels: []string{"Mon", "Tue", "Wed"}, Datasets: []chart.Dataset{{Label: "Sleep", Data: []float64{7.2, 6.8, 7.9}}}}

	apitest.AssertGolden(t, "insights-summary.golden.json", projectSummary(insightpages.SummaryView{
		Range: rng,
		Scores: []insightpages.ScoreCard{{
			Key: "body", Label: "Body", Points: 72, Verdict: "Good", Reason: "Sleep steady, water short on two days.",
			Coverage: 80, HasData: true,
			Components: []insightpages.ComponentRow{{Label: "Sleep", Earned: 30, Weight: 40, Known: true}},
		}},
		Pinned: []insightpages.PinnedCard{{
			Label: "Sleep", Value: "7.3 h", Note: "average a night", Href: metricHref("sleep"),
			Delta: insightpages.DeltaView{Pct: 4.5, Direction: 1, HasPrior: true},
			Chart: chart.Props{Data: week}, HasChart: true,
		}},
		Highlights: []string{"Best night: Wednesday, 7.9 h."},
		OnTrack:    1, Judged: 1,
	}))
	apitest.AssertGolden(t, "insights-metric.golden.json", projectMetric(insightpages.MetricView{
		Range: rng, Key: "sleep", Label: "Sleep", Headline: "7.3 h a night", Note: "From your sleep log.",
		Chart:      chart.Props{Data: week},
		Trend:      insightpages.TrendView{Direction: 1, Pct: 4.5, Word: "up", HasPrior: true},
		Comparison: insightpages.ComparisonView{CurrentLabel: "This week", CurrentValue: "7.3 h", CurrentPct: 100, PriorLabel: "Last week", PriorValue: "7.0 h", PriorPct: 96, HasPrior: true},
		Highlights: []string{"Up 4.5% on last week."},
		HasData:    true,
	}))
}
