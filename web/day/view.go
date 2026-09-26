// Package day renders "My Day": a timeline of the date down the left, a strip
// of vitals, a grid of cards, and the body.
//
// The page is built from a view model the handler fills (internal/day). The
// geometry — where a meal sits on the rail, how far a ring is drawn — is
// computed here in Go rather than in script, so the page is complete the
// moment it arrives.
package day

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Data is everything the page renders.
type Data struct {
	Date      string // YYYY-MM-DD
	DateLabel string // already localised by the handler
	IsToday   bool
	PrevHref  string
	NextHref  string
	TodayHref string
	NowLabel  string

	Vitals   []Vital
	Food     FoodCard
	Water    WaterCard
	Activity ActivityCard
	Sleep    SleepCard
	Workouts WorkoutsCard
	Streak   int
	Body     BodyCard

	Rail  Rail
	Rules []RuleRow
}

// Vital is one small arc gauge in the strip across the top.
type Vital struct {
	Key   string // catalogue suffix: day.vital.<key>
	Value string // "" renders a dash
	Unit  string
	// Fraction fills the arc; 0 when there is no value.
	Fraction float64
	Color    string // a --color-day-* token
}

// Arc is a gauge fill on a pathLength=100 circle drawn as a 270° arc.
func (v Vital) Arc() string { return arcDash(v.Fraction) }

const arcSweep = 75.0 // 270° of a 360° pathLength=100 circle

func arcDash(f float64) string {
	return fmt.Sprintf("%.2f 100", clamp01(f)*arcSweep)
}

// ArcTrack is the unfilled track behind a gauge.
func ArcTrack() string { return fmt.Sprintf("%.0f 100", arcSweep) }

// RingDash fills a full ring to f.
func RingDash(f float64) string { return fmt.Sprintf("%.2f 100", clamp01(f)*100) }

type FoodCard struct {
	Calories string
	HasGoal  bool
	Goal     string
	Macros   []Macro
}

// Macro is one of protein, carbs and fat, drawn as a ring.
type Macro struct {
	Key      string // protein | carb | fat
	Letter   string // P | C | F
	Grams    string
	Goal     string
	Fraction float64
	Color    string
	Radius   int
}

type WaterCard struct {
	TotalML  string
	TargetML string
	Litres   string
	Fraction float64
}

// FillY is where the water line sits inside the glass drawing, whose inner
// height runs from y=12 (full) to y=58 (empty).
func (w WaterCard) FillY() string {
	return fmt.Sprintf("%.1f", 58-46*clamp01(w.Fraction))
}

type ActivityCard struct {
	Rings []Ring
}

// Ring is one activity ring.
type Ring struct {
	Key      string // move | exercise | stand
	Value    string
	Goal     string
	Unit     string
	Fraction float64
	Color    string
	Radius   int
}

type SleepCard struct {
	Logged    bool
	Hours     string
	Minutes   string
	Span      string // "00:59 – 08:50"
	HasStages bool
	Stages    []SleepStageRow
	Bars      []SleepBar
	Quality   string
}

type SleepStageRow struct {
	Key     string // deep | rem | core | awake
	Minutes string
	Color   string
}

// SleepBar is one block of the hypnogram, in a 100×40 viewBox.
type SleepBar struct {
	X, Y, W string
	Color   string
}

type WorkoutsCard struct {
	Count   int
	Minutes string
	Labels  []string
}

type BodyCard struct {
	HasWeight   bool
	Weight      string
	HasBMI      bool
	BMI         string
	BMICategory string // catalogue suffix: day.bmi.<category>
}

type RuleRow struct {
	Kind    string
	At      string
	Enabled bool
}

// Rail is the day's timeline, latest at the top.
type Rail struct {
	HeightPx int
	Hours    []RailHour
	Items    []RailItem
	Markers  []RailMarker
	ShowNow  bool
	NowTopPx int
	NowLabel string
}

type RailHour struct {
	Label string
	TopPx int
}

type RailItem struct {
	TopPx  int
	Time   string
	Title  string
	Detail string
	Href   string
	Color  string
}

type RailMarker struct {
	TopPx  int
	Kind   string
	Time   string
	Passed bool
}

// Rail layout constants: an hour is 60px, and two rows are never closer than
// railRowPx so labels do not overlap however bunched the entries are.
const (
	railPxPerHour = 60
	railRowPx     = 26
	railEndHour   = 24
	railStartHour = 6
)

// RailInput is an entry before layout.
type RailInput struct {
	At     time.Time
	Title  string
	Detail string
	Href   string
	Color  string
}

// MarkerInput is a rule before layout.
type MarkerInput struct {
	At     time.Time
	Kind   string
	Passed bool
}

// BuildRail lays a day out from midnight-to-midnight inputs.
//
// The rail starts at 06:00 unless something happened earlier, and runs to
// midnight. Latest is at the top, the way the day reads when you look back on
// it from the evening.
func BuildRail(date time.Time, items []RailInput, markers []MarkerInput, now time.Time, isToday bool) Rail {
	startHour := railStartHour
	for _, it := range items {
		if h := it.At.Hour(); h < startHour {
			startHour = h
		}
	}
	span := railEndHour - startHour
	rail := Rail{HeightPx: span * railPxPerHour}

	y := func(t time.Time) int {
		mins := t.Sub(date).Minutes()
		return int(math.Round((float64(railEndHour*60) - mins) / 60 * railPxPerHour))
	}

	for h := railEndHour; h >= startHour; h -= 2 {
		rail.Hours = append(rail.Hours, RailHour{
			Label: fmt.Sprintf("%02d:00", h%24),
			TopPx: (railEndHour - h) * railPxPerHour,
		})
	}

	sorted := append([]RailInput(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.After(sorted[j].At) })
	last := -railRowPx
	for _, it := range sorted {
		top := max(y(it.At), last+railRowPx)
		last = top
		rail.Items = append(rail.Items, RailItem{
			TopPx: top, Time: it.At.Format("15:04"), Title: it.Title, Detail: it.Detail, Href: it.Href, Color: it.Color,
		})
	}

	for _, m := range markers {
		rail.Markers = append(rail.Markers, RailMarker{TopPx: y(m.At), Kind: m.Kind, Time: m.At.Format("15:04"), Passed: m.Passed})
	}

	if isToday {
		rail.ShowNow = true
		rail.NowTopPx = y(now)
		rail.NowLabel = now.Format("15:04")
	}

	// Anything spilling below the last hour stretches the rail rather than
	// being cut off.
	if len(rail.Items) > 0 {
		if bottom := rail.Items[len(rail.Items)-1].TopPx + railRowPx; bottom > rail.HeightPx {
			rail.HeightPx = bottom
		}
	}
	return rail
}

// SleepBars draws blocks into the hypnogram's 100×40 box: one row per stage,
// awake at the top and deep at the bottom.
func SleepBars(blocks []SleepBlockInput, start, end time.Time, colors map[string]string) []SleepBar {
	total := end.Sub(start).Minutes()
	if total <= 0 {
		return nil
	}
	rows := map[string]float64{"awake": 0, "rem": 10, "core": 20, "deep": 30}
	out := make([]SleepBar, 0, len(blocks))
	for _, b := range blocks {
		x := b.Start.Sub(start).Minutes() / total * 100
		w := math.Max(b.End.Sub(b.Start).Minutes()/total*100, 0.6)
		out = append(out, SleepBar{
			X: fmt.Sprintf("%.2f", x), Y: fmt.Sprintf("%.0f", rows[b.Stage]), W: fmt.Sprintf("%.2f", w),
			Color: colors[b.Stage],
		})
	}
	return out
}

// SleepBlockInput is one staged block before drawing.
type SleepBlockInput struct {
	Stage      string
	Start, End time.Time
}

func clamp01(f float64) float64 { return math.Max(0, math.Min(1, f)) }

func joinLabels(ls []string) string {
	out := ""
	for i, l := range ls {
		if i > 0 {
			out += " · "
		}
		out += l
	}
	return out
}
