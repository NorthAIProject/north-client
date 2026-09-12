package insights

import (
	"strings"
	"testing"
	"time"

	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func metricData(t *testing.T, key string, current, prior []float64) MetricData {
	t.Helper()
	m, ok := lookupMetric(key)
	if !ok {
		t.Fatalf("no metric %q", key)
	}
	rg := weekRange(t)
	prev := rg.Previous()

	pts := func(start time.Time, values []float64) []point {
		out := make([]point, 0, len(values))
		for i, v := range values {
			out = append(out, point{At: start.AddDate(0, 0, i), Value: v})
		}
		return out
	}
	return MetricData{
		Range:  rg,
		Metric: m,
		Points: pts(rg.Since, current),
		Prior:  pts(prev.Since, prior),
	}
}

func TestMetricViewAveragesAPerDayMetric(t *testing.T) {
	// Sleep is a per-day measurement: its headline is an average night, not
	// the total hours slept in the window.
	view := mustMetricView(t, metricData(t, "sleep", []float64{8, 6, 7}, nil))

	if view.Headline != "7.0h" {
		t.Errorf("Headline = %q, want %q", view.Headline, "7.0h")
	}
}

func TestMetricViewTotalsAnAccumulatingMetric(t *testing.T) {
	// Calories accumulate. Averaging them would answer a question nobody asked.
	view := mustMetricView(t, metricData(t, "burn", []float64{300, 400, 500}, nil))

	if view.Headline != "1200kcal" {
		t.Errorf("Headline = %q, want %q", view.Headline, "1200kcal")
	}
}

func TestMetricViewReportsARisingTrend(t *testing.T) {
	view := mustMetricView(t, metricData(t, "burn", []float64{600, 600}, []float64{300, 300}))

	if !view.Trend.HasPrior {
		t.Fatal("HasPrior = false with a prior window loaded")
	}
	if view.Trend.Direction != 1 {
		t.Errorf("Direction = %d, want 1", view.Trend.Direction)
	}
	if view.Trend.Word == "" {
		t.Error("trend has no word")
	}
}

func TestMetricViewCallsASmallMoveSteady(t *testing.T) {
	// A 2% wobble is not a trend. Apple writes "Trend: None" here.
	view := mustMetricView(t, metricData(t, "burn", []float64{510}, []float64{500}))

	if view.Trend.Direction != 0 {
		t.Errorf("Direction = %d, want 0 for a 2%% move", view.Trend.Direction)
	}
}

func TestMetricViewMakesNoTrendClaimWithoutAPriorWindow(t *testing.T) {
	view := mustMetricView(t, metricData(t, "burn", []float64{500}, nil))

	if view.Trend.HasPrior {
		t.Error("HasPrior = true with nothing in the prior window")
	}
}

func TestMetricViewComparesTheTwoWindowsAsBars(t *testing.T) {
	view := mustMetricView(t, metricData(t, "burn", []float64{750}, []float64{250}))

	if view.Comparison.CurrentPct != 100 {
		t.Errorf("CurrentPct = %d, want 100 — the larger window fills the bar", view.Comparison.CurrentPct)
	}
	if view.Comparison.PriorPct == 0 || view.Comparison.PriorPct >= 100 {
		t.Errorf("PriorPct = %d, want a third of the bar", view.Comparison.PriorPct)
	}
}

func TestMetricViewOfAnEmptyWindowClaimsNothing(t *testing.T) {
	view := mustMetricView(t, metricData(t, "sleep", nil, nil))

	if view.HasData {
		t.Error("HasData = true with nothing logged")
	}
	if view.Trend.HasPrior {
		t.Error("compared an empty window against an empty one")
	}
}

func TestMetricViewLinksBackToItsDomain(t *testing.T) {
	view := mustMetricView(t, metricData(t, "sleep", []float64{8}, nil))

	if view.Href != "/app/insights/body" {
		t.Errorf("Href = %q, want %q", view.Href, "/app/insights/body")
	}
}

func TestMetricViewChartsEveryBucket(t *testing.T) {
	data := metricData(t, "sleep", []float64{8, 6, 7}, nil)
	view := mustMetricView(t, data)

	if got := len(view.Chart.Data.Labels); got != len(data.Range.Buckets()) {
		t.Errorf("chart has %d labels, want %d", got, len(data.Range.Buckets()))
	}
}

func mustMetricView(t *testing.T, data MetricData) insightpages.MetricView {
	t.Helper()
	view, err := buildMetricView(data)
	if err != nil {
		t.Fatalf("buildMetricView: %v", err)
	}
	return view
}

func TestMetricViewRendersEachUnitAtItsOwnPrecision(t *testing.T) {
	// Sleep earns a decimal; millilitres and calories do not. "1580.0ml"
	// implies a measurement nobody made.
	cases := []struct {
		key     string
		values  []float64
		want    string
		comment string
	}{
		{key: "sleep", values: []float64{7.4}, want: "7.4h"},
		{key: "water", values: []float64{1580}, want: "1580ml"},
		{key: "burn", values: []float64{610}, want: "610kcal"},
		{key: "mood", values: []float64{4}, want: "4.0/5"},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			view := mustMetricView(t, metricData(t, tc.key, tc.values, nil))
			if view.Headline != tc.want {
				t.Errorf("Headline = %q, want %q", view.Headline, tc.want)
			}
		})
	}
}

func TestMetricHighlightsNameTheBucketPeriod(t *testing.T) {
	view := mustMetricView(t, metricData(t, "sleep", []float64{8, 8, 8, 0, 0, 0, 0}, nil))

	joined := strings.Join(view.Highlights, " | ")
	if joined == "" {
		t.Fatal("no highlights")
	}
	if !strings.Contains(joined, "day") {
		t.Errorf("highlights do not name the period at day grain: %s", joined)
	}
}
