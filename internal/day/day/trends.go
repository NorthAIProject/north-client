package day

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Point is one value at one time.
type Point struct {
	At    time.Time
	Value float64
}

// Series is one trend card: its points oldest first, and what to say about it.
type Series struct {
	Key    string // weight, systolic, active_energy, sleep, caffeine
	Unit   string
	Points []Point

	// Headline is the number the card leads with: the latest value for a
	// measurement, the daily average for a daily total.
	Headline float64
	// Count is how many readings or days the series holds.
	Count int
	// Window is how far back it reaches, in days.
	Window int
}

// HasData reports whether there is anything to draw.
func (s Series) HasData() bool { return len(s.Points) > 0 }

// Latest is the newest point's value.
func (s Series) Latest() float64 {
	if len(s.Points) == 0 {
		return 0
	}
	return s.Points[len(s.Points)-1].Value
}

// Measurements builds a series of individual readings (weight, blood
// pressure), headed by the latest.
func Measurements(key, unit string, window int, points []Point) Series {
	sorted := append([]Point(nil), points...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	s := Series{Key: key, Unit: unit, Points: sorted, Count: len(sorted), Window: window}
	s.Headline = s.Latest()
	return s
}

// DailyTotals builds a series of one value per day (active energy, caffeine),
// summing what falls on the same local day and headed by the average over the
// days that have any.
func DailyTotals(key, unit string, window int, points []Point, loc *time.Location) Series {
	byDay := map[time.Time]float64{}
	for _, p := range points {
		t := p.At.In(loc)
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		byDay[d] += p.Value
	}
	out := make([]Point, 0, len(byDay))
	total := 0.0
	for d, v := range byDay {
		out = append(out, Point{At: d, Value: v})
		total += v
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	s := Series{Key: key, Unit: unit, Points: out, Count: len(out), Window: window}
	if len(out) > 0 {
		s.Headline = total / float64(len(out))
	}
	return s
}

// Sparkline draws a series into a width×height box as SVG polyline points,
// oldest on the left. A flat or single-point series sits in the middle.
func Sparkline(s Series, width, height float64) string {
	if len(s.Points) == 0 {
		return ""
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range s.Points {
		lo, hi = math.Min(lo, p.Value), math.Max(hi, p.Value)
	}
	first, last := s.Points[0].At, s.Points[len(s.Points)-1].At
	span := last.Sub(first).Seconds()
	var b strings.Builder
	for i, p := range s.Points {
		x := width / 2
		if span > 0 {
			x = p.At.Sub(first).Seconds() / span * width
		}
		y := height / 2
		if hi > lo {
			// Two pixels of margin so the stroke is not clipped at the edges.
			y = 2 + (hi-p.Value)/(hi-lo)*(height-4)
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
	}
	if len(s.Points) == 1 {
		fmt.Fprintf(&b, " %.1f,%.1f", width, height/2)
	}
	return b.String()
}

// FastBar is one finished fast for the recent-fasts card.
type FastBar struct {
	StartedAt   time.Time
	Hours       float64
	TargetHours int
}

// Met reports whether the fast reached its target.
func (f FastBar) Met() bool { return f.Hours >= float64(f.TargetHours) }
