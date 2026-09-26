package day

import (
	"testing"
	"time"
)

func TestBuildRailPlacesLatestAtTheTop(t *testing.T) {
	date := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return date.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }

	rail := BuildRail(date, []RailInput{
		{At: at(9, 0), Title: "Breakfast"},
		{At: at(21, 0), Title: "Dinner"},
	}, []MarkerInput{{At: at(20, 0), Kind: "kitchen_closes"}}, nil, at(14, 46), true)

	if rail.Items[0].Title != "Dinner" || rail.Items[0].TopPx >= rail.Items[1].TopPx {
		t.Errorf("latest should be highest: %+v", rail.Items)
	}
	if got := rail.Items[0].TopPx; got != 180 { // three hours below midnight
		t.Errorf("21:00 at %dpx, want 180", got)
	}
	if !rail.ShowNow || rail.NowTopPx != 554 {
		t.Errorf("now line at %d (shown %v), want 554", rail.NowTopPx, rail.ShowNow)
	}
	if rail.Markers[0].TopPx != 240 {
		t.Errorf("20:00 marker at %d, want 240", rail.Markers[0].TopPx)
	}
	if rail.HeightPx != 18*60 {
		t.Errorf("height = %d, want 06:00 to midnight", rail.HeightPx)
	}
}

func TestBuildRailKeepsBunchedEntriesApart(t *testing.T) {
	date := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	same := date.Add(12 * time.Hour)
	rail := BuildRail(date, []RailInput{{At: same}, {At: same}, {At: same}}, nil, nil, same, false)
	for i := 1; i < len(rail.Items); i++ {
		if rail.Items[i].TopPx-rail.Items[i-1].TopPx < railRowPx {
			t.Fatalf("rows %d and %d overlap: %+v", i-1, i, rail.Items)
		}
	}
	if rail.ShowNow {
		t.Error("only today draws a now line")
	}
}

func TestBuildRailStartsEarlierForAnEarlyEntry(t *testing.T) {
	date := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	rail := BuildRail(date, []RailInput{{At: date.Add(4 * time.Hour)}}, nil, nil, date, false)
	// 04:00 to midnight, plus one row so the entry on the bottom edge fits.
	if rail.HeightPx != 20*60+railRowPx {
		t.Errorf("height = %d, want 04:00 to midnight plus a row", rail.HeightPx)
	}
}

func TestBuildRailReachesPastMidnightAndFadesIt(t *testing.T) {
	date := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	rail := BuildRail(date, []RailInput{
		{At: date.Add(25*time.Hour + 11*time.Minute), Title: "Chicken bowl"},
		{At: date.Add(12 * time.Hour), Title: "Lunch"},
		{At: date.Add(-2 * time.Hour), Title: "Late snack"},
	}, nil, nil, date.Add(12*time.Hour), false)

	// 22:00 the evening before to 02:00 after: 28 hours.
	if rail.Hours[0].Label != "02:00" || rail.Hours[len(rail.Hours)-1].Label != "22:00" {
		t.Errorf("hours run %s to %s", rail.Hours[0].Label, rail.Hours[len(rail.Hours)-1].Label)
	}
	if !rail.Items[0].Faded || rail.Items[1].Faded || !rail.Items[2].Faded {
		t.Errorf("only entries outside the date fade: %+v", rail.Items)
	}
	if rail.Items[0].TopPx != 49 { // 01:11 is 49 minutes below 02:00
		t.Errorf("01:11 at %dpx, want 49", rail.Items[0].TopPx)
	}
}

func TestBuildRailDrawsBandsBehindTheCards(t *testing.T) {
	date := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	wake := date.Add(7 * time.Hour)
	rail := BuildRail(date, nil, nil, []BandInput{
		{Start: date.Add(-time.Hour), End: &wake, Kind: "sleep"},
		{Start: date.Add(20 * time.Hour), Kind: "fast"}, // still running
	}, date.Add(22*time.Hour), true)

	if len(rail.Bands) != 2 {
		t.Fatalf("bands = %+v", rail.Bands)
	}
	// The window is 23:00 the evening before to midnight.
	sleepBand, fastBand := rail.Bands[0], rail.Bands[1]
	if sleepBand.HeightPx != 8*60 || sleepBand.TopPx != 17*60 {
		t.Errorf("sleep band = %+v", sleepBand)
	}
	if !fastBand.Open || fastBand.HeightPx != 2*60 || fastBand.TopPx != 2*60 {
		t.Errorf("fast band = %+v, want 20:00 to now", fastBand)
	}
}

func TestSleepBarsSpanTheNight(t *testing.T) {
	start := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	bars := SleepBars([]SleepBlockInput{
		{Stage: "deep", Start: start, End: start.Add(2 * time.Hour)},
		{Stage: "awake", Start: start.Add(3 * time.Hour), End: end},
	}, start, end, map[string]string{"deep": "d", "awake": "a"})
	if bars[0].X != "0.00" || bars[0].W != "50.00" || bars[0].Y != "30" {
		t.Errorf("deep bar = %+v", bars[0])
	}
	if bars[1].X != "75.00" || bars[1].Y != "0" || bars[1].Color != "a" {
		t.Errorf("awake bar = %+v", bars[1])
	}
}
