package stat

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
)

// KindStats is one activity type looked at closely: how much, how fast, how
// it is trending, its bests, and the habit around it.
type KindStats struct {
	Name string
	// Measure is "pace" (seconds per km, lower is faster) or "speed" (km/h),
	// whichever the people who do this activity think in.
	Measure string

	Sessions   int
	Seconds    int
	DistanceKm float64
	ElevationM float64
	// Indoor and Outdoor count the sessions whose provider said which;
	// the rest are unknown.
	Indoor, Outdoor int

	// AvgPace (s/km) or AvgSpeed (km/h) over the sessions with a distance
	// of at least 1 km, weighted by distance. Zero without any.
	AvgPace  float64
	AvgSpeed float64
	AvgHR    float64

	// Monthly is the pace or speed per calendar month, oldest first, for
	// months with a measured session.
	Monthly []DayValue
	// Efficiency is metres covered per heartbeat per month: the same heart
	// rate carrying someone further is aerobic fitness, whatever the pace.
	// Empty without heart rate.
	Efficiency []DayValue

	Bests []Best

	// UsualDay is the weekday most sessions fall on and UsualHour the hour
	// they start, once there are enough sessions to call it a habit.
	UsualDay  string
	UsualHour int
}

// Best is one personal best within the sessions given.
type Best struct {
	Key   string // fastest_5k, fastest_10k, longest, fastest_20k, most_climb, longest_session
	Label string
	Value float64
	// Unit is how to read Value: "s" (a time), "s/km", "km", "km/h", "m" or
	// "min".
	Unit string
	At   time.Time
}

// habitMinSessions is the fewest sessions before a usual day or hour is
// named; two sessions on a Tuesday are a coincidence.
const habitMinSessions = 3

// measuredMinM is the shortest session a pace is read from: a 200 m jog to
// the shop says nothing about how fast someone runs.
const measuredMinM = 1000

// KindDetail looks at the sessions of one activity type, named name. The
// sessions should all be of that type.
func KindDetail(name string, sessions []Session) KindStats {
	k := KindStats{Name: name, Measure: "pace", UsualHour: -1}
	if isSpeedKind(name) {
		k.Measure = "speed"
	}

	var measuredSeconds, measuredM float64
	var hrSum float64
	var hrN int
	months := map[time.Time]*monthAgg{}
	weekdays := map[int]int{}
	hours := map[int]int{}
	for _, s := range sessions {
		k.Sessions++
		k.Seconds += s.Seconds
		k.DistanceKm += s.DistanceM / 1000
		k.ElevationM += s.ElevationM
		if s.Indoor != nil {
			if *s.Indoor {
				k.Indoor++
			} else {
				k.Outdoor++
			}
		}
		if s.AvgHR > 0 {
			hrSum += s.AvgHR
			hrN++
		}
		weekdays[int(s.At.Weekday())]++
		hours[s.At.Hour()]++

		if s.DistanceM >= measuredMinM && s.Seconds > 0 {
			measuredSeconds += float64(s.Seconds)
			measuredM += s.DistanceM
			month := time.Date(s.At.Year(), s.At.Month(), 1, 0, 0, 0, 0, s.At.Location())
			m := months[month]
			if m == nil {
				m = &monthAgg{}
				months[month] = m
			}
			m.seconds += float64(s.Seconds)
			m.metres += s.DistanceM
			if s.AvgHR > 0 {
				m.perBeat = append(m.perBeat, s.DistanceM/(float64(s.Seconds)/60)/s.AvgHR)
			}
		}
	}

	if measuredM > 0 {
		k.AvgPace, k.AvgSpeed = rates(k.Measure, measuredSeconds, measuredM)
	}
	if hrN > 0 {
		k.AvgHR = util.RoundHalfUpToScale(hrSum/float64(hrN), 1)
	}
	k.Monthly, k.Efficiency = monthly(k.Measure, months)
	k.Bests = bests(name, sessions)
	if k.Sessions >= habitMinSessions {
		k.UsualDay = time.Weekday(mode(weekdays)).String()
		k.UsualHour = mode(hours)
	}
	k.DistanceKm = util.RoundHalfUpToScale(k.DistanceKm, 1)
	k.ElevationM = math.Round(k.ElevationM)
	return k
}

type monthAgg struct {
	seconds, metres float64
	perBeat         []float64
}

// isSpeedKind reports an activity people measure in km/h rather than
// minutes per km: anything on a bike.
func isSpeedKind(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "cycl") || strings.Contains(n, "bik") || strings.Contains(n, "ride")
}

// rates turns a time over a distance into a pace (s/km) or a speed (km/h).
func rates(measure string, seconds, metres float64) (pace, speed float64) {
	if measure == "speed" {
		return 0, util.RoundHalfUpToScale(metres/1000/(seconds/3600), 1)
	}
	return math.Round(seconds / (metres / 1000)), 0
}

func monthly(measure string, months map[time.Time]*monthAgg) (rate, efficiency []DayValue) {
	keys := make([]time.Time, 0, len(months))
	for m := range months {
		keys = append(keys, m)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	for _, m := range keys {
		agg := months[m]
		pace, speed := rates(measure, agg.seconds, agg.metres)
		rate = append(rate, DayValue{Day: m, Value: pace + speed})
		if len(agg.perBeat) > 0 {
			efficiency = append(efficiency, DayValue{Day: m, Value: util.RoundHalfUpToScale(mean(agg.perBeat), 2)})
		}
	}
	return rate, efficiency
}

// bests picks the records worth naming for this kind of activity.
func bests(name string, sessions []Session) []Best {
	n := strings.ToLower(name)
	var out []Best
	keep := func(b Best, better func(a, b float64) bool, value func(Session) (float64, bool)) {
		found := false
		for _, s := range sessions {
			v, ok := value(s)
			if !ok {
				continue
			}
			if !found || better(v, b.Value) {
				b.Value, b.At, found = v, s.At, true
			}
		}
		if found {
			out = append(out, b)
		}
	}
	lower := func(a, b float64) bool { return a < b }
	higher := func(a, b float64) bool { return a > b }
	pace := func(s Session) float64 { return float64(s.Seconds) / (s.DistanceM / 1000) }
	timeFor := func(km float64) func(Session) (float64, bool) {
		return func(s Session) (float64, bool) {
			if s.DistanceM < km*1000 || s.Seconds <= 0 {
				return 0, false
			}
			return math.Round(pace(s) * km), true
		}
	}
	longest := func(s Session) (float64, bool) {
		return util.RoundHalfUpToScale(s.DistanceM/1000, 1), s.DistanceM > 0
	}
	climb := func(s Session) (float64, bool) { return math.Round(s.ElevationM), s.ElevationM > 0 }

	switch {
	case strings.Contains(n, "run"):
		keep(Best{Key: "fastest_5k", Label: "Fastest 5 km", Unit: "s"}, lower, timeFor(5))
		keep(Best{Key: "fastest_10k", Label: "Fastest 10 km", Unit: "s"}, lower, timeFor(10))
		keep(Best{Key: "longest", Label: "Longest run", Unit: "km"}, higher, longest)
	case isSpeedKind(name):
		keep(Best{Key: "longest", Label: "Longest ride", Unit: "km"}, higher, longest)
		keep(Best{Key: "fastest_20k", Label: "Fastest ride over 20 km", Unit: "km/h"}, higher, func(s Session) (float64, bool) {
			if s.DistanceM < 20_000 || s.Seconds <= 0 {
				return 0, false
			}
			return util.RoundHalfUpToScale(s.DistanceM/1000/(float64(s.Seconds)/3600), 1), true
		})
		keep(Best{Key: "most_climb", Label: "Most climbing", Unit: "m"}, higher, climb)
	case strings.Contains(n, "walk") || strings.Contains(n, "hik"):
		keep(Best{Key: "longest", Label: "Longest walk", Unit: "km"}, higher, longest)
		keep(Best{Key: "most_climb", Label: "Most climbing", Unit: "m"}, higher, climb)
	case strings.Contains(n, "swim"):
		keep(Best{Key: "longest", Label: "Longest swim", Unit: "km"}, higher, longest)
	default:
		keep(Best{Key: "longest_session", Label: "Longest session", Unit: "min"}, higher, func(s Session) (float64, bool) {
			return math.Round(float64(s.Seconds) / 60), s.Seconds > 0
		})
	}
	return out
}

// mode is the most frequent key, the earliest on a tie so the answer does
// not depend on map order.
func mode(counts map[int]int) int {
	best, bestN := 0, -1
	for k, n := range counts {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	return best
}
