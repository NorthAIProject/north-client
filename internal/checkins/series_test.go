package checkins

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/users"
)

func TestBuildChartSeriesFillsTrailingDays(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	user := users.User{Timezone: "Europe/Lisbon"}

	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	list := []CheckIn{
		{LocalDate: today, Mood: 4, Energy: 3},
		{LocalDate: today.AddDate(0, 0, -2), Mood: 2, Energy: 5},
	}

	series := BuildChartSeries(user, list)
	if len(series.Days) != contextDays {
		t.Fatalf("days = %d, want %d", len(series.Days), contextDays)
	}
	if !series.HasData() {
		t.Fatal("expected chart data")
	}

	found := 0
	for _, d := range series.Days {
		if d.Mood > 0 {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("filled days = %d, want 2", found)
	}
}

func TestAverages(t *testing.T) {
	list := []CheckIn{{Mood: 4, Energy: 2}, {Mood: 2, Energy: 4}}
	avgMood, avgEnergy, count := Averages(list)
	if count != 2 {
		t.Fatalf("count = %d", count)
	}
	if avgMood != 3 {
		t.Fatalf("avg mood = %v", avgMood)
	}
	if avgEnergy != 3 {
		t.Fatalf("avg energy = %v", avgEnergy)
	}
}
