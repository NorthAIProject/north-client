package insights

import (
	"testing"
	"time"
)

func TestStreakJustReached(t *testing.T) {
	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	mondays := make([]time.Time, AreaStreakWeeks+2)
	for i := range mondays {
		mondays[i] = monday.AddDate(0, 0, 7*i)
	}
	on := AreaPoint{Points: 80, HasData: true}
	off := AreaPoint{Points: 55, HasData: true}
	none := AreaPoint{}

	cases := map[string]struct {
		trend []AreaPoint
		want  bool
	}{
		"four on track after one off":       {[]AreaPoint{off, on, on, on, on, none}, true},
		"four on track after no data":       {[]AreaPoint{none, on, on, on, on, off}, true},
		"five on track: already awarded":    {[]AreaPoint{on, on, on, on, on, on}, false},
		"three on track":                    {[]AreaPoint{off, off, on, on, on, on}, false},
		"a gap breaks it":                   {[]AreaPoint{off, on, none, on, on, on}, false},
		"the week in progress never counts": {[]AreaPoint{off, off, on, on, on, on}, false},
		"exactly the line counts":           {[]AreaPoint{off, {Points: 70, HasData: true}, on, on, on, off}, true},
	}
	for name, c := range cases {
		start, ok := streakJustReached(c.trend, mondays)
		if ok != c.want {
			t.Errorf("%s: ok = %v", name, ok)
		}
		if ok && !start.Equal(mondays[1]) {
			t.Errorf("%s: run starts %s, want %s", name, start.Format(time.DateOnly), mondays[1].Format(time.DateOnly))
		}
	}
}
