package care

import (
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	carepages "github.com/NorthAIProject/north-client/web/care"
)

func buildView(snap Snapshot) (carepages.Instruments, error) {
	labels := make([]string, len(snap.HydrationSeries))
	values := make([]float64, len(snap.HydrationSeries))
	for i, d := range snap.HydrationSeries {
		labels[i] = d.Label
		values[i] = d.Value
	}

	sleepLabels := make([]string, len(snap.SleepSeries))
	sleepValues := make([]float64, len(snap.SleepSeries))
	for i, d := range snap.SleepSeries {
		sleepLabels[i] = d.Label
		sleepValues[i] = d.Value
	}

	var habitGauge map[string]any
	if snap.HasHabits() {
		raw, err := viz.GaugeOptionJSON("Adherence", snap.HabitRate)
		if err != nil {
			return carepages.Instruments{}, err
		}
		habitGauge, err = viz.UnmarshalOption(raw)
		if err != nil {
			return carepages.Instruments{}, err
		}
	}

	return carepages.Instruments{
		HydrationChart: viz.Bar("care-hydration", "Water (ml)", labels, values),
		SleepChart:     viz.SingleLine("care-sleep", "Hours", sleepLabels, sleepValues, nil, nil),
		HabitGauge:     habitGauge,
		HabitRate:      snap.HabitRate,
		HasHydration:   snap.HasHydrationChart(),
		HasSleep:       snap.HasSleepChart(),
		HasHabits:      snap.HasHabits(),
		DueCount:       snap.DueCount,
	}, nil
}

func pageData(snap Snapshot, inst carepages.Instruments) carepages.Data {
	return carepages.Data{
		DueReminders:   snap.DueReminders,
		AllReminders:   snap.AllReminders,
		CheckedInToday: snap.CheckedInToday,
		Water:          snap.Water,
		WaterEntries:   snap.WaterEntries,
		LastNight:      snap.LastNight,
		SleptLastNight: snap.SleptLastNight,
		Habits:         snap.Habits,
		Instruments:    inst,
	}
}
