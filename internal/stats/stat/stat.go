// Package stat is the arithmetic behind the stats pages: sleep, cardio,
// eating, and the patterns that link them. Pure functions over plain
// inputs, so every number on the page can be checked in a test.
package stat

import (
	"math"
	"sort"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
)

// DayValue is one figure for one day (or week).
type DayValue struct {
	Day   time.Time
	Value float64
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	sum := 0.0
	for _, x := range xs {
		sum += (x - m) * (x - m)
	}
	return math.Sqrt(sum / float64(len(xs)))
}

func isWeekend(t time.Time) bool { return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday }

// WeekStart is the Monday of t's week, at midnight in t's zone.
func WeekStart(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// Weekly sums values per week from since to until, oldest first, with empty
// weeks included so a chart shows the gaps.
func Weekly(values []DayValue, since, until time.Time) []DayValue {
	totals := map[time.Time]float64{}
	for _, v := range values {
		totals[WeekStart(v.Day)] += v.Value
	}
	var out []DayValue
	for w := WeekStart(since); w.Before(until); w = w.AddDate(0, 0, 7) {
		out = append(out, DayValue{Day: w, Value: util.RoundHalfUpToScale(totals[w], 1)})
	}
	return out
}

// ClockMinutes is minutes after midnight, but for a bedtime-like time the
// small hours count past 24:00 so 23:30 and 00:30 average to midnight, not
// noon.
func ClockMinutes(t time.Time, night bool) float64 {
	m := float64(t.Hour()*60 + t.Minute())
	if night && t.Hour() < 12 {
		m += 24 * 60
	}
	return m
}

// Clock renders minutes after midnight (possibly past 24h) as "HH:MM".
func Clock(minutes float64) string {
	m := (int(math.Round(minutes))%(24*60) + 24*60) % (24 * 60)
	return time.Date(0, 1, 1, m/60, m%60, 0, 0, time.UTC).Format("15:04")
}

// ---------------------------------------------------------------------------
// Sleep
// ---------------------------------------------------------------------------

// Night is one night's sleep, filed under the morning it ended.
type Night struct {
	Date    time.Time
	Minutes int
	Start   *time.Time
	End     *time.Time
	// Stages is minutes per stage (deep, rem, core, awake); empty for a
	// manual log.
	Stages  map[string]int
	Quality *int
}

// SleepStats is sleep over a window.
type SleepStats struct {
	Nights        []Night // oldest first
	TargetMinutes int
	AvgMinutes    int
	// DebtMinutes is the shortfall against the target over the last seven
	// nights logged; nights over target do not pay it back.
	DebtMinutes int
	// Bedtime and wake time: the average, and how much they vary night to
	// night (a standard deviation, in minutes). Consistency matters about as
	// much as duration.
	AvgBedtime    string
	AvgWake       string
	BedtimeSpread int
	WakeSpread    int
	HasTimes      bool
	Best, Worst   *Night
	WeekdayAvg    int
	WeekendAvg    int
	// StageShare is each stage's share of time asleep, 0-1, over nights with
	// stages.
	StageShare  map[string]float64
	NightsOnTgt int
}

// Sleep reads a window of nights against a target.
func Sleep(nights []Night, target int) SleepStats {
	sorted := append([]Night(nil), nights...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })
	st := SleepStats{Nights: sorted, TargetMinutes: target}
	if len(sorted) == 0 {
		return st
	}
	var mins, weekday, weekend, beds, wakes []float64
	stageTotals := map[string]float64{}
	asleep := 0.0
	for i := range sorted {
		n := &sorted[i]
		mins = append(mins, float64(n.Minutes))
		if isWeekend(n.Date) {
			weekend = append(weekend, float64(n.Minutes))
		} else {
			weekday = append(weekday, float64(n.Minutes))
		}
		if n.Minutes >= target {
			st.NightsOnTgt++
		}
		if n.Start != nil && n.End != nil {
			beds = append(beds, ClockMinutes(*n.Start, true))
			wakes = append(wakes, ClockMinutes(*n.End, false))
		}
		for stage, m := range n.Stages {
			stageTotals[stage] += float64(m)
			if stage != "awake" {
				asleep += float64(m)
			}
		}
		if st.Best == nil || n.Minutes > st.Best.Minutes {
			st.Best = n
		}
		if st.Worst == nil || n.Minutes < st.Worst.Minutes {
			st.Worst = n
		}
	}
	st.AvgMinutes = int(math.Round(mean(mins)))
	st.WeekdayAvg = int(math.Round(mean(weekday)))
	st.WeekendAvg = int(math.Round(mean(weekend)))
	for _, n := range sorted[max(0, len(sorted)-7):] {
		st.DebtMinutes += max(0, target-n.Minutes)
	}
	if len(beds) > 0 {
		st.HasTimes = true
		st.AvgBedtime, st.AvgWake = Clock(mean(beds)), Clock(mean(wakes))
		st.BedtimeSpread = int(math.Round(stddev(beds)))
		st.WakeSpread = int(math.Round(stddev(wakes)))
	}
	if asleep > 0 {
		st.StageShare = map[string]float64{}
		for stage, m := range stageTotals {
			if stage != "awake" {
				st.StageShare[stage] = util.RoundHalfUpToScale(m/asleep, 2)
			}
		}
	}
	return st
}

// ---------------------------------------------------------------------------
// Cardio
// ---------------------------------------------------------------------------

// Session is one finished cardio session.
type Session struct {
	Code      string
	Name      string
	At        time.Time
	Seconds   int
	DistanceM float64
	Kcal      float64

	// What a device measured; zero is not measured.
	AvgHR      float64
	ElevationM float64
	// Indoor is nil when the provider did not say.
	Indoor *bool
}

// IsRun reports whether a session is a run, which gets pace and records.
func (s Session) IsRun() bool {
	return s.Code == "running" || len(s.Code) > 8 && s.Code[:8] == "running_"
}

// PaceSeconds is seconds per kilometre, 0 without a distance.
func (s Session) PaceSeconds() float64 {
	if s.DistanceM <= 0 || s.Seconds <= 0 {
		return 0
	}
	return float64(s.Seconds) / (s.DistanceM / 1000)
}

// Kind is one activity type over the window.
type Kind struct {
	Name       string
	Sessions   int
	Seconds    int
	DistanceKm float64

	// Measure, AvgPace, AvgSpeed and AvgHR are as in KindStats, over the
	// window.
	Measure  string
	AvgPace  float64
	AvgSpeed float64
	AvgHR    float64
}

// Runs is what a runner looks at.
type Runs struct {
	Count      int
	DistanceKm float64
	// AvgPace and BestPace are seconds per km over runs of at least 1 km.
	AvgPace   float64
	BestPace  float64
	LongestKm float64
	// Best5K is the fastest 5 km at the pace of a run at least that long,
	// in seconds; 0 when no run reached 5 km.
	Best5K float64
}

// CardioStats is cardio over a window.
type CardioStats struct {
	Sessions   int
	Seconds    int
	DistanceKm float64
	Kcal       float64
	ByKind     []Kind
	WeeklyKm   []DayValue
	WeeklyMins []DayValue
	Runs       Runs
	Recent     []Session // newest first
}

// Cardio adds a window's sessions up.
func Cardio(sessions []Session, since, until time.Time) CardioStats {
	st := CardioStats{}
	kinds := map[string]*Kind{}
	var order []string
	var km, mins []DayValue
	var paces []float64
	for _, s := range sessions {
		st.Sessions++
		st.Seconds += s.Seconds
		st.DistanceKm += s.DistanceM / 1000
		st.Kcal += s.Kcal
		k, ok := kinds[s.Name]
		if !ok {
			k = &Kind{Name: s.Name}
			kinds[s.Name] = k
			order = append(order, s.Name)
		}
		k.Sessions++
		k.Seconds += s.Seconds
		k.DistanceKm += s.DistanceM / 1000
		km = append(km, DayValue{Day: s.At, Value: s.DistanceM / 1000})
		mins = append(mins, DayValue{Day: s.At, Value: float64(s.Seconds) / 60})

		if s.IsRun() {
			r := &st.Runs
			r.Count++
			r.DistanceKm += s.DistanceM / 1000
			r.LongestKm = math.Max(r.LongestKm, s.DistanceM/1000)
			if pace := s.PaceSeconds(); pace > 0 && s.DistanceM >= 1000 {
				paces = append(paces, pace)
				if r.BestPace == 0 || pace < r.BestPace {
					r.BestPace = pace
				}
				if s.DistanceM >= 5000 && (r.Best5K == 0 || pace*5 < r.Best5K) {
					r.Best5K = pace * 5
				}
			}
		}
	}
	st.DistanceKm = util.RoundHalfUpToScale(st.DistanceKm, 1)
	st.Kcal = math.Round(st.Kcal)
	st.Runs.DistanceKm = util.RoundHalfUpToScale(st.Runs.DistanceKm, 1)
	st.Runs.LongestKm = util.RoundHalfUpToScale(st.Runs.LongestKm, 1)
	st.Runs.AvgPace = math.Round(mean(paces))
	st.Runs.BestPace = math.Round(st.Runs.BestPace)
	st.Runs.Best5K = math.Round(st.Runs.Best5K)
	byName := map[string][]Session{}
	for _, s := range sessions {
		byName[s.Name] = append(byName[s.Name], s)
	}
	for _, name := range order {
		k := kinds[name]
		k.DistanceKm = util.RoundHalfUpToScale(k.DistanceKm, 1)
		detail := KindDetail(name, byName[name])
		k.Measure, k.AvgPace, k.AvgSpeed, k.AvgHR = detail.Measure, detail.AvgPace, detail.AvgSpeed, detail.AvgHR
		st.ByKind = append(st.ByKind, *k)
	}
	sort.SliceStable(st.ByKind, func(i, j int) bool { return st.ByKind[i].Seconds > st.ByKind[j].Seconds })
	st.WeeklyKm = Weekly(km, since, until)
	st.WeeklyMins = Weekly(mins, since, until)
	st.Recent = append([]Session(nil), sessions...)
	sort.Slice(st.Recent, func(i, j int) bool { return st.Recent[i].At.After(st.Recent[j].At) })
	if len(st.Recent) > 10 {
		st.Recent = st.Recent[:10]
	}
	return st
}

// ---------------------------------------------------------------------------
// Eating
// ---------------------------------------------------------------------------

// FoodEntry is one thing eaten.
type FoodEntry struct {
	At      time.Time // in the reader's zone
	Date    time.Time
	Label   string
	Kcal    float64
	Protein float64
	Carb    float64
	Fat     float64
}

// Food is one food over the window.
type Food struct {
	Label string
	Count int
	Kcal  float64
}

// Slot is a time of day and the share of calories eaten in it.
type Slot struct {
	Key   string // morning, midday, afternoon, evening, late
	Kcal  float64
	Share float64
}

// EatingStats is eating over a window.
type EatingStats struct {
	DaysLogged int
	AvgKcal    float64
	AvgProtein float64
	AvgCarb    float64
	AvgFat     float64
	// Against the calculator's targets; zero goals mean none set.
	GoalKcal     float64
	GoalProtein  float64
	OnTargetDays int // calories within 10% of the goal
	ProteinDays  int // protein at 90% of the goal or more
	ProteinPerKg float64
	TopFoods     []Food
	BySlot       []Slot
	WeekdayKcal  float64
	WeekendKcal  float64
	// LateDays counts days whose last entry came at or after lateHour.
	LateDays int
	Daily    []DayValue // calories per day, oldest first
}

// Slots in eating order, with the hour each starts.
var slots = []struct {
	key  string
	from int
}{{"morning", 0}, {"midday", 11}, {"afternoon", 15}, {"evening", 18}, {"late", 21}}

func slotFor(t time.Time) string {
	key := slots[0].key
	for _, s := range slots {
		if t.Hour() >= s.from {
			key = s.key
		}
	}
	return key
}

// Eating reads a window of food entries against goals. weightKg is 0 when
// unknown; lateHour is when eating counts as late (the kitchen-closes rule,
// or 21).
func Eating(entries []FoodEntry, goalKcal, goalProtein, weightKg float64, lateHour int) EatingStats {
	st := EatingStats{GoalKcal: goalKcal, GoalProtein: goalProtein}
	type dayTotals struct {
		kcal, protein, carb, fat float64
		last                     time.Time
	}
	days := map[time.Time]*dayTotals{}
	foods := map[string]*Food{}
	slotKcal := map[string]float64{}
	total := 0.0
	for _, e := range entries {
		d, ok := days[e.Date]
		if !ok {
			d = &dayTotals{}
			days[e.Date] = d
		}
		d.kcal += e.Kcal
		d.protein += e.Protein
		d.carb += e.Carb
		d.fat += e.Fat
		if e.At.After(d.last) {
			d.last = e.At
		}
		f, ok := foods[e.Label]
		if !ok {
			f = &Food{Label: e.Label}
			foods[e.Label] = f
		}
		f.Count++
		f.Kcal += e.Kcal
		slotKcal[slotFor(e.At)] += e.Kcal
		total += e.Kcal
	}
	st.DaysLogged = len(days)
	if st.DaysLogged == 0 {
		return st
	}
	var kcal, protein, carb, fat, weekday, weekend []float64
	for date, d := range days {
		kcal = append(kcal, d.kcal)
		protein = append(protein, d.protein)
		carb = append(carb, d.carb)
		fat = append(fat, d.fat)
		if isWeekend(date) {
			weekend = append(weekend, d.kcal)
		} else {
			weekday = append(weekday, d.kcal)
		}
		if goalKcal > 0 && math.Abs(d.kcal-goalKcal) <= goalKcal*0.1 {
			st.OnTargetDays++
		}
		if goalProtein > 0 && d.protein >= goalProtein*0.9 {
			st.ProteinDays++
		}
		if d.last.Hour() >= lateHour {
			st.LateDays++
		}
		st.Daily = append(st.Daily, DayValue{Day: date, Value: math.Round(d.kcal)})
	}
	sort.Slice(st.Daily, func(i, j int) bool { return st.Daily[i].Day.Before(st.Daily[j].Day) })
	st.AvgKcal = math.Round(mean(kcal))
	st.AvgProtein = math.Round(mean(protein))
	st.AvgCarb = math.Round(mean(carb))
	st.AvgFat = math.Round(mean(fat))
	st.WeekdayKcal = math.Round(mean(weekday))
	st.WeekendKcal = math.Round(mean(weekend))
	if weightKg > 0 {
		st.ProteinPerKg = util.RoundHalfUpToScale(st.AvgProtein/weightKg, 1)
	}
	for _, f := range foods {
		f.Kcal = math.Round(f.Kcal)
		st.TopFoods = append(st.TopFoods, *f)
	}
	sort.Slice(st.TopFoods, func(i, j int) bool {
		if st.TopFoods[i].Count != st.TopFoods[j].Count {
			return st.TopFoods[i].Count > st.TopFoods[j].Count
		}
		return st.TopFoods[i].Label < st.TopFoods[j].Label
	})
	if len(st.TopFoods) > 8 {
		st.TopFoods = st.TopFoods[:8]
	}
	for _, s := range slots {
		share := 0.0
		if total > 0 {
			share = util.RoundHalfUpToScale(slotKcal[s.key]/total, 2)
		}
		st.BySlot = append(st.BySlot, Slot{Key: s.key, Kcal: math.Round(slotKcal[s.key]), Share: share})
	}
	return st
}
