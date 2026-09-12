package score

import (
	"math"
	"strconv"
	"strings"

	"github.com/NorthAIProject/north-client/internal/habits/habit"
	"github.com/NorthAIProject/north-client/internal/hydration/hydration"
	"github.com/NorthAIProject/north-client/internal/sleep/sleep"
)

// Weights for the body domain. They sum to 100, which is what makes Coverage
// readable as a percentage — see TestBodyWeightsSumToOneHundred.
const (
	bodyWeightSleep     = 40
	bodyWeightBedtime   = 20
	bodyWeightHydration = 25
	bodyWeightHabits    = 15
)

// Body reads sleep, bedtime regularity, water and habit adherence over a
// window. windowDays is the length of the window, needed because the loaders
// return a row per logged day rather than a row per day: a week with two
// drinks recorded is two days of water, not a full week of it.
func Body(nights []sleep.Log, days []hydration.Day, tracked []habit.Stats, windowDays int) Score {
	return New("body", []Component{
		sleepDurationComponent(nights),
		bedtimeConsistencyComponent(nights),
		hydrationComponent(days, windowDays),
		habitsComponent(tracked),
	})
}

// restfulBand is the nightly range that earns full marks, in minutes. Outside
// it the score falls off linearly and reaches zero three hours out, so a
// six-hour night reads as worse than an eight without reading as a disaster.
const (
	restfulMinMinutes = 7 * 60
	restfulMaxMinutes = 9 * 60
	restfulFalloff    = 3 * 60
)

func sleepDurationComponent(nights []sleep.Log) Component {
	c := Component{Key: "sleep_duration", Weight: bodyWeightSleep}

	var total, n float64
	for _, l := range nights {
		if l.DurationMinutes <= 0 {
			continue
		}
		total += float64(l.DurationMinutes)
		n++
	}
	if n == 0 {
		return c
	}

	c.Known = true
	c.Earned = award(c.Weight, bandFit(total/n, restfulMinMinutes, restfulMaxMinutes, restfulFalloff))
	return c
}

// minBedtimes is how many logged bedtimes it takes before calling somebody's
// routine consistent or not. Two nights is a coincidence.
const minBedtimes = 3

// Bedtime spread earning full marks, and the spread at which it earns nothing.
const (
	steadyBedtimeStdDev  = 30.0
	erraticBedtimeStdDev = 120.0
)

func bedtimeConsistencyComponent(nights []sleep.Log) Component {
	c := Component{Key: "bedtime_consistency", Weight: bodyWeightBedtime}

	minutes := make([]float64, 0, len(nights))
	for _, l := range nights {
		if m, ok := eveningMinutes(l.Bedtime); ok {
			minutes = append(minutes, m)
		}
	}
	if len(minutes) < minBedtimes {
		return c
	}

	c.Known = true
	c.Earned = award(c.Weight, 1-progress(stdDev(minutes), steadyBedtimeStdDev, erraticBedtimeStdDev))
	return c
}

// eveningMinutes turns "HH:MM" into minutes on a timeline where the evening
// runs past midnight, so 23:40 and 00:10 read as half an hour apart rather
// than as the whole day they are apart on a clock face.
func eveningMinutes(bedtime string) (float64, bool) {
	h, m, ok := parseHHMM(bedtime)
	if !ok {
		return 0, false
	}
	total := float64(h*60 + m)
	if h < 12 {
		total += 24 * 60
	}
	return total, true
}

func parseHHMM(v string) (int, int, bool) {
	hh, mm, found := strings.Cut(strings.TrimSpace(v), ":")
	if !found {
		return 0, 0, false
	}
	h, err := strconv.Atoi(hh)
	if err != nil || h < 0 || h > 23 {
		return 0, 0, false
	}
	m, err := strconv.Atoi(mm)
	if err != nil || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func hydrationComponent(days []hydration.Day, windowDays int) Component {
	c := Component{Key: "hydration", Weight: bodyWeightHydration}
	if windowDays <= 0 {
		return c
	}

	var total, target float64
	var logged int
	for _, d := range days {
		if d.Entries == 0 || d.TargetML <= 0 {
			continue
		}
		total += float64(d.TotalML)
		target = float64(d.TargetML)
		logged++
	}
	if logged == 0 {
		return c
	}

	// Measured against the whole window, not against the days they drank on.
	// Averaging the logged days alone would score somebody who drank once and
	// hit target that day as perfectly hydrated for the month.
	c.Known = true
	c.Earned = award(c.Weight, total/(target*float64(windowDays)))
	return c
}

func habitsComponent(tracked []habit.Stats) Component {
	c := Component{Key: "habits", Weight: bodyWeightHabits}

	var kept, scheduled int
	for _, s := range tracked {
		// Stats.Rate reports 100 for a habit with nothing scheduled, which is
		// right on its own page and wrong here: it would let a dormant habit
		// lift the score. Count the days instead.
		if s.Scheduled <= 0 {
			continue
		}
		kept += s.Kept
		scheduled += s.Scheduled
	}
	if scheduled == 0 {
		return c
	}

	c.Known = true
	c.Earned = award(c.Weight, float64(kept)/float64(scheduled))
	return c
}

// award turns a 0-1 share into points, clamped so a window that beat its
// target cannot earn more than the component is worth.
func award(weight int, share float64) int {
	switch {
	case math.IsNaN(share) || share <= 0:
		return 0
	case share >= 1:
		return weight
	}
	return int(math.Round(share * float64(weight)))
}

// bandFit is 1 inside [min, max] and falls linearly to 0 falloff beyond it.
func bandFit(v, min, max, falloff float64) float64 {
	switch {
	case v < min:
		return 1 - (min-v)/falloff
	case v > max:
		return 1 - (v-max)/falloff
	default:
		return 1
	}
}

// progress is how far v has travelled from good to bad, as 0-1.
func progress(v, good, bad float64) float64 {
	if v <= good {
		return 0
	}
	if v >= bad {
		return 1
	}
	return (v - good) / (bad - good)
}

func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	var variance float64
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	return math.Sqrt(variance / float64(len(values)))
}
