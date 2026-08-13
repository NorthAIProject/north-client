package checkins

import (
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/users"
	checkinpages "github.com/NorthAIProject/north-client/web/checkins"
)

func buildInstruments(user users.User, list []CheckIn) (checkinpages.Instruments, error) {
	series := BuildChartSeries(user, list)
	labels := make([]string, len(series.Days))
	mood := make([]int, len(series.Days))
	energy := make([]int, len(series.Days))
	heatmap := make([]viz.HeatmapCell, len(series.Days))
	for i, d := range series.Days {
		labels[i] = d.Label
		mood[i] = d.Mood
		energy[i] = d.Energy
		heatmap[i] = viz.HeatmapCell{Label: d.Label, Value: d.Mood}
	}

	var heatmapOption map[string]any
	if series.HasData() {
		raw, err := viz.HeatmapJSON("Mood", heatmap)
		if err != nil {
			return checkinpages.Instruments{}, err
		}
		heatmapOption, err = viz.UnmarshalOption(raw)
		if err != nil {
			return checkinpages.Instruments{}, err
		}
	}

	avgMood, avgEnergy, count := Averages(list)

	return checkinpages.Instruments{
		MoodChart:    viz.MoodEnergyLine("checkins-mood-energy", labels, mood, energy),
		Heatmap:      heatmapOption,
		HasData:      series.HasData(),
		AvgMood:      avgMood,
		AvgEnergy:    avgEnergy,
		CheckInCount: count,
	}, nil
}
