package viz

import (
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

// MoodEnergyLine builds a dual-line Chart.js config with y-axis 1–5.
func MoodEnergyLine(id string, labels []string, mood, energy []int) chart.Props {
	moodData := nullableSeries(mood)
	energyData := nullableSeries(energy)
	yMin := 1.0
	yMax := 5.0
	beginZero := false

	return chart.Props{
		ID:      id,
		Variant: chart.VariantLine,
		Class:   "min-h-48 w-full",
		RawConfig: map[string]any{
			"type": "line",
			"data": map[string]any{
				"labels": labels,
				"datasets": []any{
					map[string]any{
						"label":           "Mood",
						"data":            moodData,
						"tension":         0.35,
						"borderWidth":     2,
						"pointRadius":     3,
						"spanGaps":        false,
						"borderColor":     "var(--north-signal)",
						"backgroundColor": "color-mix(in oklch, var(--north-signal) 12%, transparent)",
						"fill":            false,
					},
					map[string]any{
						"label":           "Energy",
						"data":            energyData,
						"tension":         0.35,
						"borderWidth":     2,
						"pointRadius":     3,
						"spanGaps":        false,
						"borderColor":     "var(--muted-foreground)",
						"backgroundColor": "color-mix(in oklch, var(--muted-foreground) 12%, transparent)",
						"fill":            false,
					},
				},
			},
			"options": map[string]any{
				"responsive":          true,
				"maintainAspectRatio": false,
				"plugins": map[string]any{
					"legend": map[string]any{"display": true},
				},
			},
			"showLegend":  true,
			"showXGrid":   false,
			"showYGrid":   true,
			"showXLabels": true,
			"showYLabels": true,
			"yMin":        &yMin,
			"yMax":        &yMax,
			"beginAtZero": &beginZero,
		},
	}
}

// SingleLine builds one series with optional y bounds.
func SingleLine(id, label string, labels []string, values []float64, yMin, yMax *float64) chart.Props {
	beginZero := yMin == nil
	return chart.Props{
		ID:      id,
		Variant: chart.VariantLine,
		Class:   "min-h-48 w-full",
		Data: chart.Data{
			Labels: labels,
			Datasets: []chart.Dataset{
				{
					Label:           label,
					Data:            values,
					BorderWidth:     2,
					Tension:         0.35,
					BorderColor:     "var(--north-signal)",
					BackgroundColor: "color-mix(in oklch, var(--north-signal) 12%, transparent)",
					Fill:            false,
				},
			},
		},
		ShowLegend:  false,
		ShowXGrid:   false,
		ShowYGrid:   true,
		ShowXLabels: true,
		ShowYLabels: true,
		YMin:        yMin,
		YMax:        yMax,
		BeginAtZero: &beginZero,
	}
}

// Bar renders vertical bars.
func Bar(id, datasetLabel string, labels []string, totals []float64) chart.Props {
	beginZero := true
	return chart.Props{
		ID:      id,
		Variant: chart.VariantBar,
		Class:   "min-h-48 w-full",
		Data: chart.Data{
			Labels: labels,
			Datasets: []chart.Dataset{
				{
					Label:           datasetLabel,
					Data:            totals,
					BorderWidth:     0,
					BackgroundColor: "var(--north-signal)",
				},
			},
		},
		ShowLegend:  false,
		ShowXGrid:   false,
		ShowYGrid:   true,
		ShowXLabels: true,
		ShowYLabels: true,
		BeginAtZero: &beginZero,
	}
}

func nullableSeries(values []int) []any {
	out := make([]any, len(values))
	for i, v := range values {
		if v <= 0 {
			out[i] = nil
			continue
		}
		out[i] = v
	}
	return out
}

// Sparkline is a bar chart stripped to its shape, for a summary card that
// already states the number beside it.
//
// Everything Bar shows and this does not — axes, grid, legend — would repeat
// the card's own headline and leave the bars too short to read.
func Sparkline(id string, labels []string, values []float64) chart.Props {
	beginZero := true
	return chart.Props{
		ID:      id,
		Variant: chart.VariantBar,
		Class:   "h-12 w-full",
		Data: chart.Data{
			Labels: labels,
			Datasets: []chart.Dataset{
				{
					Label:           "",
					Data:            values,
					BorderWidth:     0,
					BackgroundColor: "var(--north-signal)",
				},
			},
		},
		ShowLegend:  false,
		ShowXGrid:   false,
		ShowYGrid:   false,
		ShowXLabels: false,
		ShowYLabels: false,
		BeginAtZero: &beginZero,
	}
}

// BarSeries is one named run of values in a grouped bar chart.
type BarSeries struct {
	Label  string
	Values []float64
}

// barSeriesColours are the fills a grouped chart cycles through. Two series in
// one colour is one series with a confusing shape, so this is the palette the
// legend is read against rather than a decoration.
var barSeriesColours = []string{
	"var(--north-signal)",
	"var(--muted-foreground)",
	"var(--north-ember)",
}

// GroupedBar draws several named series over shared labels.
//
// Grouped rather than stacked: the question these charts answer is how two
// runs compare bar by bar, and a stack answers how they sum, which is a
// different question and hides the smaller of the two.
func GroupedBar(id string, labels []string, series []BarSeries) chart.Props {
	beginZero := true

	datasets := make([]chart.Dataset, 0, len(series))
	for i, s := range series {
		datasets = append(datasets, chart.Dataset{
			Label:           s.Label,
			Data:            s.Values,
			BorderWidth:     0,
			BackgroundColor: barSeriesColours[i%len(barSeriesColours)],
		})
	}

	return chart.Props{
		ID:          id,
		Variant:     chart.VariantBar,
		Class:       "min-h-48 w-full",
		Data:        chart.Data{Labels: labels, Datasets: datasets},
		ShowLegend:  true,
		ShowXGrid:   false,
		ShowYGrid:   true,
		ShowXLabels: true,
		ShowYLabels: true,
		BeginAtZero: &beginZero,
	}
}
