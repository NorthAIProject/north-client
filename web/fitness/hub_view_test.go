package fitness

import (
	"testing"
	"time"
)

func TestCompact(t *testing.T) {
	cases := map[float64]string{
		950:       "950",
		6500:      "6.5k",
		30_900:    "30.9k",
		10_000:    "10k",
		1_200_000: "1.2M",
	}
	for in, want := range cases {
		if got := compact(in); got != want {
			t.Errorf("compact(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRecentDetailOnlyShowsWhatTheSessionHas(t *testing.T) {
	walk := RecentRow{Duration: 111 * time.Minute, DistanceKm: 2.2}
	got := recentDetail(walk)
	want := []string{"1h 51m", "2.2 km", "50:27/km"}
	if len(got) != len(want) {
		t.Fatalf("detail = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("detail = %v, want %v", got, want)
		}
	}

	lift := RecentRow{Duration: 50 * time.Minute, Calories: 310}
	if got := recentDetail(lift); len(got) != 2 || got[0] != "50m" || got[1] != "310 kcal" {
		t.Fatalf("lift detail = %v", got)
	}
}

func TestRecentTitleDropsTheIntensity(t *testing.T) {
	if got := recentTitle(RecentRow{Name: "Walking (brisk pace)"}); got != "Walking" {
		t.Fatalf("title = %q", got)
	}
	if got := recentTitle(RecentRow{Name: "custom_thing"}); got != "Custom thing" {
		t.Fatalf("title = %q", got)
	}
}

func TestTrendSkipsTodaysPartialZero(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	tr := Trend{Days: []TrendDay{{day(22), 5300}, {day(23), 3700}, {day(24), 6500}, {day(25), 0}}}

	latest, ok := tr.latest()
	if !ok || latest.Value != 6500 {
		t.Fatalf("latest = %+v", latest)
	}
	rows := tr.recentRows(5)
	if len(rows) != 3 || rows[0].Delta != 2800 || rows[1].Delta != -1600 || rows[2].HasDelta {
		t.Fatalf("rows = %+v", rows)
	}
	if got := tr.perDay(); got != 5166.666666666667 {
		t.Fatalf("perDay = %v", got)
	}
}

func TestBarHeightScalesAgainstTheBusiestDay(t *testing.T) {
	w := WeekView{Bars: []WeekBar{{Minutes: 0}, {Minutes: 30}, {Minutes: 120}}}
	if got := w.barHeight(w.Bars[2]); got != "100%" {
		t.Fatalf("top bar = %s", got)
	}
	if got := w.barHeight(w.Bars[1]); got != "25%" {
		t.Fatalf("quarter bar = %s", got)
	}
	if got := w.barHeight(w.Bars[0]); got != "6%" {
		t.Fatalf("empty bar = %s", got)
	}
}
