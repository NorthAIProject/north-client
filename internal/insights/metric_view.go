package insights

import (
	"fmt"
	"math"

	"github.com/NorthAIProject/north-client/internal/insights/highlight"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// steadyBand is how far a metric may move against the previous window before
// the page calls it a direction. Below it the chip reads "Steady", which is
// the honest reading of a number that wobbled.
const steadyBand = 0.05

func buildMetricView(data MetricData) (insightpages.MetricView, error) {
	m := data.Metric
	labels := bucketLabels(data.Range)

	series := bucketed(data.Range, data.Points)
	if m.Mean {
		series = bucketedMean(data.Range, data.Points)
	}

	current := aggregate(m, data.Points)
	prior := aggregate(m, data.Prior)

	view := insightpages.MetricView{
		Range:     rangeView(data.Range),
		Key:       m.Key,
		Label:     m.Label,
		Headline:  formatMetric(m, current),
		Note:      metricNote(m, len(data.Points)),
		Href:      m.Href,
		HrefLabel: "See the whole domain",
		Chart:     viz.Bar("insights-metric-"+m.Key, m.Label, labels, series),
		HasData:   len(data.Points) > 0,
	}

	view.Trend = trendView(m, current, prior, len(data.Prior) > 0)
	view.Comparison = comparisonView(m, current, prior, len(data.Prior) > 0)
	view.Highlights = highlight.Find(highlight.Input{
		Series: []highlight.Series{{
			Label: m.Label, Unit: m.Unit, Decimals: m.Decimals,
			Period: periodNoun(data.Range), Labels: labels, Values: series,
		}},
		Comparisons: metricComparisons(m, current, prior, len(data.Prior) > 0),
	}, maxHighlights)

	return view, nil
}

// aggregate reduces a window to the one number the page leads with: an average
// for a per-day measurement, a total for anything that accumulates.
func aggregate(m metric, points []point) float64 {
	if len(points) == 0 {
		return 0
	}

	var total float64
	for _, p := range points {
		total += p.Value
	}
	if m.Mean {
		return total / float64(len(points))
	}
	return total
}

func trendView(m metric, current, prior float64, hasPrior bool) insightpages.TrendView {
	if !hasPrior || prior <= 0 {
		return insightpages.TrendView{}
	}

	delta := (current - prior) / prior
	out := insightpages.TrendView{Pct: math.Abs(delta) * 100, HasPrior: true}

	switch {
	case math.Abs(delta) < steadyBand:
		out.Word = "Steady"
	case delta > 0:
		out.Direction = 1
		out.Word = fmt.Sprintf("Up %.0f%%", out.Pct)
	default:
		out.Direction = -1
		out.Word = fmt.Sprintf("Down %.0f%%", out.Pct)
	}

	// Better flips the reading for a metric where less is the good news. No
	// metric here is one yet, but the chip colours off Direction and a future
	// resting-heart-rate page must not be told that rising is progress.
	if m.Better < 0 {
		out.Direction = -out.Direction
	}
	return out
}

func comparisonView(m metric, current, prior float64, hasPrior bool) insightpages.ComparisonView {
	if !hasPrior {
		return insightpages.ComparisonView{}
	}

	// Both bars are drawn against the larger of the two, so the longer one
	// always fills and the shorter reads as its true fraction of it.
	scale := math.Max(current, prior)
	if scale <= 0 {
		return insightpages.ComparisonView{}
	}

	return insightpages.ComparisonView{
		CurrentLabel: "This window",
		CurrentValue: formatMetric(m, current),
		CurrentPct:   int(math.Round(current / scale * 100)),
		PriorLabel:   "The one before",
		PriorValue:   formatMetric(m, prior),
		PriorPct:     int(math.Round(prior / scale * 100)),
		HasPrior:     true,
	}
}

func metricComparisons(m metric, current, prior float64, hasPrior bool) []highlight.Comparison {
	if !hasPrior {
		return nil
	}
	return []highlight.Comparison{{Label: m.Label, Current: current, Prior: prior}}
}

// formatMetric renders a metric's value at its own precision.
func formatMetric(m metric, v float64) string {
	return fmt.Sprintf("%.*f%s", m.Decimals, v, m.Unit)
}

func metricNote(m metric, n int) string {
	switch {
	case n == 0:
		return ""
	case m.Mean:
		return fmt.Sprintf("average across %d logged days", n)
	default:
		return fmt.Sprintf("across %d entries", n)
	}
}
