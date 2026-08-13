package checkins

import (
	"time"

	"github.com/NorthAIProject/north-client/internal/users"
)

// ChartDay is one calendar day in the mood/energy chart window.
type ChartDay struct {
	Label  string
	Mood   int // zero means no check-in that day
	Energy int
}

// ChartSeries is the trailing check-in window for charts.
type ChartSeries struct {
	Days []ChartDay
}

func (s ChartSeries) HasData() bool {
	for _, d := range s.Days {
		if d.Mood > 0 || d.Energy > 0 {
			return true
		}
	}
	return false
}

// Averages returns mean mood and energy over the given check-ins.
func Averages(list []CheckIn) (avgMood, avgEnergy float64, count int) {
	if len(list) == 0 {
		return 0, 0, 0
	}
	var mood, energy int
	for _, c := range list {
		mood += c.Mood
		energy += c.Energy
	}
	n := float64(len(list))
	return float64(mood) / n, float64(energy) / n, len(list)
}

// BuildChartSeries maps check-ins onto trailing calendar days.
func BuildChartSeries(user users.User, list []CheckIn) ChartSeries {
	loc := user.Location()
	days := trailingCalendarDays(loc, contextDays)
	byDate := make(map[string]CheckIn, len(list))
	for _, c := range list {
		key := c.LocalDate.In(loc).Format("2006-01-02")
		byDate[key] = c
	}

	out := make([]ChartDay, len(days))
	for i, d := range days {
		key := d.Format("2006-01-02")
		day := ChartDay{Label: d.Format("2 Jan")}
		if c, ok := byDate[key]; ok {
			day.Mood = c.Mood
			day.Energy = c.Energy
		}
		out[i] = day
	}
	return ChartSeries{Days: out}
}

func trailingCalendarDays(loc *time.Location, count int) []time.Time {
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	out := make([]time.Time, count)
	for i := range count {
		out[i] = today.AddDate(0, 0, -(count - 1 - i))
	}
	return out
}
