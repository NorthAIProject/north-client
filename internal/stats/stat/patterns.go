package stat

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
)

// DayFacts is what is known about one local date, for finding patterns.
// Pointers are unknown, not zero.
type DayFacts struct {
	Date time.Time
	// SleepMinutes is the night that ended on the morning of Date.
	SleepMinutes *int
	// LateCaffeine is caffeine after the cutoff rule on Date.
	LateCaffeine bool
	HadCaffeine  bool
	ScreenMin    *int
	// LateEating is food logged at or after the kitchen-closes rule on Date.
	LateEating bool
	AteLogged  bool
	Energy     *int // check-in, 1-5
	Mood       *int // check-in, 1-5
	Trained    bool // a workout or lifted sets on Date
	Soreness   int  // sore regions recorded on Date
}

// Group is one side of a comparison.
type Group struct {
	Label string
	Mean  float64
	N     int
}

// Finding is one pattern, stated plainly: two groups of days and how a
// measure differed between them.
type Finding struct {
	Key    string
	Title  string
	Detail string
	Unit   string
	A, B   Group
	// Diff is A's mean minus B's.
	Diff float64
	// Strength ranks findings: the difference relative to the measure's
	// usual size, so 40 minutes of sleep and one point of mood compare.
	Strength float64
}

// MinGroup is the fewest days either side needs before a difference is
// reported. Three is few, and the page says so; fewer is noise.
const MinGroup = 3

type split struct {
	key, unit              string
	aLabel, bLabel         string
	scale                  float64 // the measure's typical size, for Strength
	title                  func(diff float64) string
	detail                 func(a, b Group) string
	value                  func(prev, cur DayFacts) (float64, bool)
	inA                    func(prev, cur DayFacts) (bool, bool)
	requirePrevForGrouping bool
}

func mins(v float64) string {
	v = math.Abs(v)
	if v >= 60 {
		return fmt.Sprintf("%dh %02dm", int(v)/60, int(v)%60)
	}
	return fmt.Sprintf("%.0f min", v)
}

func more(diff float64) string {
	if diff >= 0 {
		return "more"
	}
	return "less"
}

func higher(diff float64) string {
	if diff >= 0 {
		return "higher"
	}
	return "lower"
}

var splits = []split{
	{
		key: "sleep_late_caffeine", unit: "min", scale: 60,
		aLabel: "After caffeine past your cutoff", bLabel: "Other nights",
		title: func(d float64) string {
			return fmt.Sprintf("You sleep %s %s after late caffeine", mins(d), more(d))
		},
		detail: func(a, b Group) string {
			return fmt.Sprintf("%s on %d nights after caffeine past your cutoff, %s on %d other nights.",
				mins(a.Mean), a.N, mins(b.Mean), b.N)
		},
		value:                  func(_, cur DayFacts) (float64, bool) { return sleepOf(cur) },
		inA:                    func(prev, _ DayFacts) (bool, bool) { return prev.LateCaffeine, true },
		requirePrevForGrouping: true,
	},
	{
		key: "sleep_late_eating", unit: "min", scale: 60,
		aLabel: "After eating late", bLabel: "After an earlier last meal",
		title: func(d float64) string {
			return fmt.Sprintf("You sleep %s %s after eating late", mins(d), more(d))
		},
		detail: func(a, b Group) string {
			return fmt.Sprintf("%s on %d nights after food past your kitchen-closes time, %s on %d nights after an earlier last meal.",
				mins(a.Mean), a.N, mins(b.Mean), b.N)
		},
		value: func(_, cur DayFacts) (float64, bool) { return sleepOf(cur) },
		inA: func(prev, _ DayFacts) (bool, bool) {
			return prev.LateEating, prev.AteLogged
		},
		requirePrevForGrouping: true,
	},
	{
		key: "energy_sleep", unit: "/5", scale: 1,
		aLabel: "After 7 hours or more", bLabel: "After less than 7 hours",
		title: func(d float64) string {
			return fmt.Sprintf("Your energy is %.1f points %s after 7+ hours of sleep", math.Abs(d), higher(d))
		},
		detail: func(a, b Group) string {
			return fmt.Sprintf("Check-in energy averages %.1f on %d days after 7+ hours, %.1f on %d days after less.",
				a.Mean, a.N, b.Mean, b.N)
		},
		value: func(_, cur DayFacts) (float64, bool) {
			if cur.Energy == nil {
				return 0, false
			}
			return float64(*cur.Energy), true
		},
		inA: func(_, cur DayFacts) (bool, bool) {
			if cur.SleepMinutes == nil {
				return false, false
			}
			return *cur.SleepMinutes >= 7*60, true
		},
	},
	{
		key: "mood_training", unit: "/5", scale: 1,
		aLabel: "Training days", bLabel: "Rest days",
		title: func(d float64) string {
			return fmt.Sprintf("Your mood is %.1f points %s on training days", math.Abs(d), higher(d))
		},
		detail: func(a, b Group) string {
			return fmt.Sprintf("Check-in mood averages %.1f on %d training days, %.1f on %d rest days.",
				a.Mean, a.N, b.Mean, b.N)
		},
		value: func(_, cur DayFacts) (float64, bool) {
			if cur.Mood == nil {
				return 0, false
			}
			return float64(*cur.Mood), true
		},
		inA: func(_, cur DayFacts) (bool, bool) { return cur.Trained, true },
	},
	{
		key: "energy_training", unit: "/5", scale: 1,
		aLabel: "Training days", bLabel: "Rest days",
		title: func(d float64) string {
			return fmt.Sprintf("Your energy is %.1f points %s on training days", math.Abs(d), higher(d))
		},
		detail: func(a, b Group) string {
			return fmt.Sprintf("Check-in energy averages %.1f on %d training days, %.1f on %d rest days.",
				a.Mean, a.N, b.Mean, b.N)
		},
		value: func(_, cur DayFacts) (float64, bool) {
			if cur.Energy == nil {
				return 0, false
			}
			return float64(*cur.Energy), true
		},
		inA: func(_, cur DayFacts) (bool, bool) { return cur.Trained, true },
	},
}

func sleepOf(d DayFacts) (float64, bool) {
	if d.SleepMinutes == nil {
		return 0, false
	}
	return float64(*d.SleepMinutes), true
}

// Patterns compares groups of days and returns what differed, strongest
// first. days must be consecutive-by-date or have gaps; a "previous day" is
// the calendar day before, when it is present.
func Patterns(days []DayFacts) []Finding {
	byDate := map[string]DayFacts{}
	for _, d := range days {
		byDate[d.Date.Format(time.DateOnly)] = d
	}
	screen := screenSplit(days)
	all := append(append([]split(nil), splits...), screen...)

	var out []Finding
	for _, sp := range all {
		var a, b []float64
		for _, cur := range days {
			prev, hasPrev := byDate[cur.Date.AddDate(0, 0, -1).Format(time.DateOnly)]
			if sp.requirePrevForGrouping && !hasPrev {
				continue
			}
			v, ok := sp.value(prev, cur)
			if !ok {
				continue
			}
			inA, known := sp.inA(prev, cur)
			if !known {
				continue
			}
			if inA {
				a = append(a, v)
			} else {
				b = append(b, v)
			}
		}
		if len(a) < MinGroup || len(b) < MinGroup {
			continue
		}
		ga := Group{Label: sp.aLabel, Mean: util.RoundHalfUpToScale(mean(a), 1), N: len(a)}
		gb := Group{Label: sp.bLabel, Mean: util.RoundHalfUpToScale(mean(b), 1), N: len(b)}
		diff := util.RoundHalfUpToScale(ga.Mean-gb.Mean, 1)
		if diff == 0 {
			continue
		}
		out = append(out, Finding{
			Key: sp.key, Title: sp.title(diff), Detail: sp.detail(ga, gb), Unit: sp.unit,
			A: ga, B: gb, Diff: diff, Strength: math.Abs(diff) / sp.scale,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Strength > out[j].Strength })
	return out
}

// screenSplit compares sleep after high- and low-screen days, split at the
// median of the days that have a figure.
func screenSplit(days []DayFacts) []split {
	var values []float64
	for _, d := range days {
		if d.ScreenMin != nil {
			values = append(values, float64(*d.ScreenMin))
		}
	}
	if len(values) < 2*MinGroup {
		return nil
	}
	sort.Float64s(values)
	median := values[len(values)/2]
	return []split{{
		key: "sleep_screen", unit: "min", scale: 60,
		aLabel: "After heavy screen days", bLabel: "After lighter ones",
		title: func(d float64) string {
			return fmt.Sprintf("You sleep %s %s after days with more than %s of screen time", mins(d), more(d), mins(median))
		},
		detail: func(a, b Group) string {
			return fmt.Sprintf("%s on %d nights after heavier screen days, %s on %d nights after lighter ones.",
				mins(a.Mean), a.N, mins(b.Mean), b.N)
		},
		value: func(_, cur DayFacts) (float64, bool) { return sleepOf(cur) },
		inA: func(prev, _ DayFacts) (bool, bool) {
			if prev.ScreenMin == nil {
				return false, false
			}
			return float64(*prev.ScreenMin) >= median, true
		},
		requirePrevForGrouping: true,
	}}
}
