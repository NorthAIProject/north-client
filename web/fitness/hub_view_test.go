package fitness

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
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
		if got := compact(context.Background(), in); got != want {
			t.Errorf("compact(%v) = %q, want %q", in, got, want)
		}
	}
}

// Spanish and Portuguese read a decimal comma, and a whole compact count
// still drops its ",0".
func TestCompactUsesTheReadersDecimalMark(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), "es")
	cases := map[float64]string{6500: "6,5k", 10_000: "10k", 1_200_000: "1,2M"}
	for in, want := range cases {
		if got := compact(ctx, in); got != want {
			t.Errorf("compact(es, %v) = %q, want %q", in, got, want)
		}
	}
	if got := signed(ctx, 0.5, 1); got != "+0,5" {
		t.Errorf("signed(es, 0.5) = %q", got)
	}
}

func TestRecentDetailOnlyShowsWhatTheSessionHas(t *testing.T) {
	walk := RecentRow{Duration: 111 * time.Minute, DistanceKm: 2.2}
	got := recentDetail(context.Background(), walk)
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
	if got := recentDetail(context.Background(), lift); len(got) != 2 || got[0] != "50m" || got[1] != "310 kcal" {
		t.Fatalf("lift detail = %v", got)
	}
}

func TestRecentTitleDropsTheIntensity(t *testing.T) {
	if got := recentTitle(context.Background(), RecentRow{Name: "Walking (brisk pace)"}); got != "Walking" {
		t.Fatalf("title = %q", got)
	}
	if got := recentTitle(context.Background(), RecentRow{Name: "custom_thing"}); got != "Custom thing" {
		t.Fatalf("title = %q", got)
	}
}

func TestTrendSkipsTodaysPartialZero(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	tr := Trend{Days: []TrendDay{{Day: day(22), Value: 5300}, {Day: day(23), Value: 3700}, {Day: day(24), Value: 6500}, {Day: day(25), Value: 0}}}

	latest, ok := latestReading(tr)
	if !ok || latest.Value != 6500 {
		t.Fatalf("latest = %+v", latest)
	}
	rows := recentRows(tr, 5)
	if len(rows) != 3 || rows[0].Delta != 2800 || rows[1].Delta != -1600 || rows[2].HasDelta {
		t.Fatalf("rows = %+v", rows)
	}
	if got := perDay(tr); got != 5166.666666666667 {
		t.Fatalf("perDay = %v", got)
	}
}

func TestBarHeightScalesAgainstTheBusiestDay(t *testing.T) {
	w := WeekView{Bars: []WeekBar{{Minutes: 0}, {Minutes: 30}, {Minutes: 120}}}
	if got := barHeight(w, w.Bars[2]); got != "100%" {
		t.Fatalf("top bar = %s", got)
	}
	if got := barHeight(w, w.Bars[1]); got != "25%" {
		t.Fatalf("quarter bar = %s", got)
	}
	if got := barHeight(w, w.Bars[0]); got != "6%" {
		t.Fatalf("empty bar = %s", got)
	}
}
