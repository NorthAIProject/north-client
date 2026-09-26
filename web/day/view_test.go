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
	}, []MarkerInput{{At: at(20, 0), Kind: "kitchen_closes"}}, at(14, 46), true)

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
	rail := BuildRail(date, []RailInput{{At: same}, {At: same}, {At: same}}, nil, same, false)
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
	rail := BuildRail(date, []RailInput{{At: date.Add(4 * time.Hour)}}, nil, date, false)
	// 04:00 to midnight, plus one row so the entry on the bottom edge fits.
	if rail.HeightPx != 20*60+railRowPx {
		t.Errorf("height = %d, want 04:00 to midnight plus a row", rail.HeightPx)
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
