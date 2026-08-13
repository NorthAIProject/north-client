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
