package mind

import (
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/users"
	mindpages "github.com/NorthAIProject/north-client/web/mind"
)

func buildInstruments(user users.User, list []checkins.CheckIn) mindpages.Instruments {
	series := checkins.BuildChartSeries(user, list)
	labels := make([]string, len(series.Days))
	mood := make([]int, len(series.Days))
	energy := make([]int, len(series.Days))
	for i, d := range series.Days {
		labels[i] = d.Label
		mood[i] = d.Mood
		energy[i] = d.Energy
	}

	avgMood, avgEnergy, count := checkins.Averages(list)

	return mindpages.Instruments{
		MoodChart:    viz.MoodEnergyLine("mind-mood-energy", labels, mood, energy),
		HasData:      series.HasData(),
		AvgMood:      avgMood,
		AvgEnergy:    avgEnergy,
		CheckInCount: count,
	}
}
