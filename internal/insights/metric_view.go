package insights

import (
	"context"
	"fmt"
	"math"

	"github.com/NorthAIProject/north-client/internal/insights/highlight"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// steadyBand is how far a metric may move against the previous window before
// the page calls it a direction. Below it the chip reads "Steady", which is
// the honest reading of a number that wobbled.
const steadyBand = 0.05

func buildMetricView(ctx context.Context, data MetricData) (insightpages.MetricView, error) {
	m := data.Metric
	label := metricLabel(ctx, m)
	labels := bucketLabels(ctx, data.Range)

	series := bucketed(data.Range, data.Points)
	if m.Mean {
		series = bucketedMean(data.Range, data.Points)
	}

	current := aggregate(m, data.Points)
	prior := aggregate(m, data.Prior)

	view := insightpages.MetricView{
		Range:     rangeView(ctx, data.Range),
		Key:       m.Key,
		Label:     label,
		Headline:  formatMetric(m, current),
		Note:      metricNote(ctx, m, len(data.Points)),
		Href:      m.Href,
		HrefLabel: i18n.T(ctx, "insights.metric.domainLink"),
		Chart:     viz.Bar("insights-metric-"+m.Key, label, labels, series),
		HasData:   len(data.Points) > 0,
		Health:    m.Health,
	}
	if data.Usual != nil {
		u := usualView(ctx, m, *data.Usual)
		view.Usual = &u
	}

	view.Trend = trendView(ctx, m, current, prior, len(data.Prior) > 0)
	view.Comparison = comparisonView(ctx, m, current, prior, len(data.Prior) > 0)
	view.Highlights = highlight.Find(highlight.Input{
		Series: []highlight.Series{{
			Label: label, Unit: m.Unit, Decimals: m.Decimals,
			Period: periodNoun(data.Range), Labels: labels, Values: series,
		}},
		Comparisons: metricComparisons(label, current, prior, len(data.Prior) > 0),
		Locale:      i18n.LocaleFrom(ctx),
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

func trendView(ctx context.Context, m metric, current, prior float64, hasPrior bool) insightpages.TrendView {
	if !hasPrior || prior <= 0 {
		return insightpages.TrendView{}
	}

	delta := (current - prior) / prior
	out := insightpages.TrendView{Pct: math.Abs(delta) * 100, HasPrior: true}

	switch {
	case math.Abs(delta) < steadyBand:
		out.Word = i18n.T(ctx, "insights.metric.trend.steady")
	case delta > 0:
		out.Direction = 1
		out.Word = i18n.Tf(ctx, "insights.metric.trend.up", out.Pct)
	default:
		out.Direction = -1
		out.Word = i18n.Tf(ctx, "insights.metric.trend.down", out.Pct)
	}

	// Better flips the reading for a metric where less is the good news. No
	// metric here is one yet, but the chip colours off Direction and a future
	// resting-heart-rate page must not be told that rising is progress.
	if m.Better < 0 {
		out.Direction = -out.Direction
	}
	return out
}

func comparisonView(ctx context.Context, m metric, current, prior float64, hasPrior bool) insightpages.ComparisonView {
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
		CurrentLabel: i18n.T(ctx, "insights.metric.compare.current"),
		CurrentValue: formatMetric(m, current),
		CurrentPct:   int(math.Round(current / scale * 100)),
		PriorLabel:   i18n.T(ctx, "insights.metric.compare.prior"),
		PriorValue:   formatMetric(m, prior),
		PriorPct:     int(math.Round(prior / scale * 100)),
		HasPrior:     true,
	}
}

func metricComparisons(label string, current, prior float64, hasPrior bool) []highlight.Comparison {
	if !hasPrior {
		return nil
	}
	return []highlight.Comparison{{Label: label, Current: current, Prior: prior}}
}

// usualView words a latest day against the person's usual range: the mean
// give or take one standard deviation, which about two days in three fall in.
//
// The state follows the numbers as shown, not as stored: a resting heart rate
// of 52 against a range shown as 52–54 is within it, whatever the decimals
// behind the rounding say.
//
// The sentence is in ctx's language; a ctx with no reader reads English, which
// is what the coach's context lines want.
func usualView(ctx context.Context, m metric, u Usual) insightpages.UsualView {
	lowValue := math.Max(u.Baseline.Mean-u.Baseline.SD, 0)
	highValue := u.Baseline.Mean + u.Baseline.SD
	low, high := formatMetric(m, lowValue), formatMetric(m, highValue)
	state := u.State
	shown := func(v float64) float64 { return roundTo(v, m.Decimals) }
	if latest := shown(u.Latest.Value); latest >= shown(lowValue) && latest <= shown(highValue) {
		state = stat.Usual
	}
	lead := ""
	switch state {
	case stat.Above, stat.Usual, stat.Below:
		lead = i18n.T(ctx, "insights.usual."+state)
	}
	return insightpages.UsualView{
		Day: u.Latest.At, Latest: u.Latest.Value,
		Mean: u.Baseline.Mean, SD: u.Baseline.SD, Low: low, High: high,
		Days: u.Baseline.Days, Z: math.Round(u.Z*100) / 100, State: state,
		Text: i18n.Tf(ctx, "insights.usual.text",
			lead, formatMetric(m, u.Latest.Value), i18n.Date(ctx, u.Latest.At, "Mon 2 Jan"), low, high),
	}
}

// roundTo rounds v to decimals places, the way formatMetric shows it.
func roundTo(v float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	return math.Round(v*scale) / scale
}

// formatMetric renders a metric's value at its own precision.
func formatMetric(m metric, v float64) string {
	return fmt.Sprintf("%.*f%s", m.Decimals, v, m.Unit)
}

func metricNote(ctx context.Context, m metric, n int) string {
	switch {
	case n == 0:
		return ""
	case m.Mean:
		return i18n.Tf(ctx, "insights.metric.note.mean", n)
	default:
		return i18n.Tf(ctx, "insights.metric.note.total", n)
	}
}

// metricLabel is a metric's name in the reader's language. m.Label stays the
// English name, which the coach's context lines are written in.
func metricLabel(ctx context.Context, m metric) string {
	return translated(ctx, "insights.metric.name."+m.Key, m.Label)
}
