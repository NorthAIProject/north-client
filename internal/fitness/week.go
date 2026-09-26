package fitness

import (
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/health"
)

// recentLimit is how many sessions the hub lists. Enough to see the shape of a
// week; the full history lives on the activity page.
const recentLimit = 5

// stepsWindow and vo2Window are how far back the trend cards look. Steps move
// daily, so two weeks shows a pattern; VO2 max moves over weeks, so a month.
const (
	stepsWindow = 15
	vo2Window   = 30
)

// WeekDay is one day of the "this week" strip.
type WeekDay struct {
	Label   string
	Minutes float64
	IsToday bool
}

// Week is the rollup the hub leads with: what the last seven days added up to.
type Week struct {
	Days       []WeekDay
	Sessions   int
	Minutes    float64
	DistanceKm float64
}

// RecentSession is one completed session, reduced to what a list row shows.
type RecentSession struct {
	ActivityCode string
	Name         string
	Category     string
	Source       string
	EndedAt      time.Time
	Duration     time.Duration
	DistanceKm   float64
	Calories     float64
}

// DailyValue is one day's total of a device metric.
type DailyValue struct {
	Day   time.Time
	Value float64
}

// WeightReading is the latest weight and how it moved since the one before.
type WeightReading struct {
	Kg       float64
	DeltaKg  float64
	HasDelta bool
	At       time.Time
}

// buildWeek sums completed sessions into the trailing seven days ending today.
func buildWeek(loc *time.Location, now time.Time, sessions []activity.Session) Week {
	days := trailingDaysAt(loc, now, calorieWindow)
	index := make(map[string]int, len(days))
	week := Week{Days: make([]WeekDay, len(days))}
	for i, d := range days {
		index[dayKey(d)] = i
		week.Days[i] = WeekDay{Label: d.Format("Mon")[:2], IsToday: i == len(days)-1}
	}

	for _, s := range sessions {
		if s.Status != activity.StatusCompleted || s.EndedAt == nil {
			continue
		}
		i, ok := index[dayKey(s.EndedAt.In(loc))]
		if !ok {
			continue
		}
		minutes := s.Elapsed(*s.EndedAt).Minutes()
		week.Days[i].Minutes += minutes
		week.Minutes += minutes
		week.Sessions++
		if s.DistanceM != nil {
			week.DistanceKm += *s.DistanceM / 1000
		}
	}
	return week
}

// buildRecent keeps the newest completed sessions, newest first.
func buildRecent(sessions []activity.Session) []RecentSession {
	out := make([]RecentSession, 0, recentLimit)
	for _, s := range sessions {
		if len(out) == recentLimit {
			break
		}
		if s.Status != activity.StatusCompleted || s.EndedAt == nil {
			continue
		}
		r := RecentSession{
			ActivityCode: s.ActivityCode,
			Name:         s.ActivityCode,
			Source:       s.Source,
			EndedAt:      *s.EndedAt,
			Duration:     s.Elapsed(*s.EndedAt),
		}
		if met, ok := activity.LookupMET(s.ActivityCode); ok {
			r.Name = met.Name
			r.Category = met.Category
		}
		if s.DistanceM != nil {
			r.DistanceKm = *s.DistanceM / 1000
		}
		if s.CaloriesBurned != nil {
			r.Calories = *s.CaloriesBurned
		}
		out = append(out, r)
	}
	return out
}

// dailyTotals sums readings into one value per local day, oldest first, with
// a zero for every day in the window that had no reading.
func dailyTotals(loc *time.Location, now time.Time, days int, readings []health.Stored) []DailyValue {
	window := trailingDaysAt(loc, now, days)
	index := make(map[string]int, len(window))
	out := make([]DailyValue, len(window))
	for i, d := range window {
		index[dayKey(d)] = i
		out[i] = DailyValue{Day: d}
	}
	for _, r := range readings {
		if i, ok := index[dayKey(r.StartedAt.In(loc))]; ok {
			out[i].Value += r.Value
		}
	}
	return out
}

// dailyMeans averages readings per local day, oldest first, skipping days
// without one. A VO2 max estimate is a level, not an amount: two readings on
// one day are two opinions of the same number, not twice the fitness.
func dailyMeans(loc *time.Location, readings []health.Stored) []DailyValue {
	type acc struct {
		day   time.Time
		sum   float64
		count int
	}
	byDay := map[string]*acc{}
	var order []string
	// Readings arrive newest first; walk backwards so the output is oldest first.
	for i := len(readings) - 1; i >= 0; i-- {
		r := readings[i]
		local := r.StartedAt.In(loc)
		key := dayKey(local)
		a, ok := byDay[key]
		if !ok {
			a = &acc{day: time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)}
			byDay[key] = a
			order = append(order, key)
		}
		a.sum += r.Value
		a.count++
	}
	out := make([]DailyValue, 0, len(order))
	for _, key := range order {
		a := byDay[key]
		out = append(out, DailyValue{Day: a.day, Value: a.sum / float64(a.count)})
	}
	return out
}

// latestWeight reads the newest weight and the change from the one before.
// History is newest first.
func latestWeight(history []biometrics.Biometric) (WeightReading, bool) {
	if len(history) == 0 || history[0].WeightKg <= 0 {
		return WeightReading{}, false
	}
	w := WeightReading{Kg: history[0].WeightKg, At: history[0].CreatedAt}
	for _, prev := range history[1:] {
		if prev.WeightKg > 0 {
			w.DeltaKg = w.Kg - prev.WeightKg
			w.HasDelta = true
			break
		}
	}
	return w, true
}

func trailingDaysAt(loc *time.Location, now time.Time, count int) []time.Time {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	out := make([]time.Time, count)
	for i := range count {
		out[i] = today.AddDate(0, 0, -(count - 1 - i))
	}
	return out
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }
