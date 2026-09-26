package day

import (
	"strings"
	"testing"
	"time"
)

func TestDailyTotalsSumsEachDayAndAveragesDaysWithData(t *testing.T) {
	d := func(day, h int) time.Time { return time.Date(2026, 9, day, h, 0, 0, 0, time.UTC) }
	s := DailyTotals("caffeine", "mg", 14, []Point{
		{At: d(24, 9), Value: 100}, {At: d(24, 14), Value: 63}, {At: d(26, 8), Value: 100},
	}, time.UTC)
	if s.Count != 2 || s.Points[0].Value != 163 || s.Headline != 131.5 {
		t.Errorf("series = %+v", s)
	}
}

func TestMeasurementsLeadWithTheLatest(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 9, day, 8, 0, 0, 0, time.UTC) }
	s := Measurements("weight", "kg", 84, []Point{{At: d(20), Value: 84.5}, {At: d(26), Value: 83.9}, {At: d(1), Value: 86}})
	if s.Headline != 83.9 || s.Points[0].Value != 86 {
		t.Errorf("series = %+v", s)
	}
}

func TestSparklineSpansTheBox(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	line := Sparkline(Series{Points: []Point{{At: d(1), Value: 10}, {At: d(3), Value: 20}}}, 100, 40)
	if line != "0.0,38.0 100.0,2.0" {
		t.Errorf("sparkline = %q", line)
	}
	one := Sparkline(Series{Points: []Point{{At: d(1), Value: 5}}}, 100, 40)
	if !strings.HasPrefix(one, "50.0,20.0") {
		t.Errorf("single point = %q", one)
	}
	if Sparkline(Series{}, 100, 40) != "" {
		t.Error("no points, no line")
	}
}
