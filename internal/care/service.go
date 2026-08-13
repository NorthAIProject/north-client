package care

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	hydrationWindow = 7
	sleepWindow     = 7
)

// Options wires the slices this page composes.
type Options struct {
	Reminders *meals.MealReminderService
	CheckIns  *checkins.Service
	Hydration *hydration.Service
	Sleep     *sleep.Service
	Habits    *habits.Service
}

type Service struct {
	reminders *meals.MealReminderService
	checkins  *checkins.Service
	hydration *hydration.Service
	sleep     *sleep.Service
	habits    *habits.Service
}

func NewService(opts Options) *Service {
	return &Service{
		reminders: opts.Reminders,
		checkins:  opts.CheckIns,
		hydration: opts.Hydration,
		sleep:     opts.Sleep,
		habits:    opts.Habits,
	}
}

// DayPoint is one label/value pair for instrument charts.
type DayPoint struct {
	Label string
	Value float64
}

// Snapshot is everything the care page renders.
type Snapshot struct {
	DueReminders   []meals.Reminder
	AllReminders   []meals.Reminder
	CheckedInToday bool
	Water          hydration.Day
	WaterEntries   []hydration.Entry
	LastNight      sleep.Log
	SleptLastNight bool
	Habits         []habits.Stats

	HydrationSeries []DayPoint
	SleepSeries     []DayPoint
	HabitRate       int
	DueCount        int
}

func (s Snapshot) HasHydrationChart() bool {
	for _, d := range s.HydrationSeries {
		if d.Value > 0 {
			return true
		}
	}
	return s.Water.TotalML > 0
}

func (s Snapshot) HasSleepChart() bool {
	for _, d := range s.SleepSeries {
		if d.Value > 0 {
			return true
		}
	}
	return s.SleptLastNight
}

func (s Snapshot) HasHabits() bool { return len(s.Habits) > 0 }

// Load gathers today's care view. Real errors fail the page.
func (s *Service) Load(ctx context.Context, user users.User) (Snapshot, error) {
	var snap Snapshot

	due, err := s.reminders.DueNow(ctx, user.ID, time.Now().In(user.Location()))
	if err != nil {
		return Snapshot{}, err
	}
	snap.DueReminders = due
	snap.DueCount = len(due)

	allReminders, err := s.reminders.List(ctx, user.ID)
	if err != nil {
		return Snapshot{}, err
	}
	snap.AllReminders = allReminders

	if _, err = s.checkins.Today(ctx, user); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			snap.CheckedInToday = false
		} else {
			return Snapshot{}, err
		}
	} else {
		snap.CheckedInToday = true
	}

	water, err := s.hydration.Today(ctx, user)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Water = water

	waterEntries, err := s.hydration.TodayEntries(ctx, user)
	if err != nil {
		return Snapshot{}, err
	}
	snap.WaterEntries = waterEntries

	lastNight, slept, err := s.sleep.Today(ctx, user)
	if err != nil {
		return Snapshot{}, err
	}
	snap.LastNight = lastNight
	snap.SleptLastNight = slept

	habitStats, err := s.habits.Today(ctx, user)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Habits = habitStats
	snap.HabitRate = summarizeHabitRate(habitStats)

	recentWater, err := s.hydration.RecentDays(ctx, user, hydrationWindow)
	if err != nil {
		return Snapshot{}, err
	}
	snap.HydrationSeries = buildHydrationSeries(user, recentWater)

	sleepLogs, err := s.sleep.Recent(ctx, user, sleepWindow)
	if err != nil {
		return Snapshot{}, err
	}
	snap.SleepSeries = buildSleepSeries(user, sleepLogs)

	return snap, nil
}

func summarizeHabitRate(stats []habits.Stats) int {
	if len(stats) == 0 {
		return 0
	}
	var kept, scheduled int
	for _, st := range stats {
		kept += st.Kept
		scheduled += st.Scheduled
	}
	if scheduled == 0 {
		return 100
	}
	return kept * 100 / scheduled
}

func buildHydrationSeries(user users.User, recent []hydration.Day) []DayPoint {
	loc := user.Location()
	days := trailingDays(loc, hydrationWindow)
	byDate := make(map[string]hydration.Day, len(recent))
	for _, d := range recent {
		byDate[d.Date.In(loc).Format("2006-01-02")] = d
	}
	out := make([]DayPoint, len(days))
	for i, d := range days {
		total := 0.0
		if row, ok := byDate[d.Format("2006-01-02")]; ok {
			total = float64(row.TotalML)
		}
		out[i] = DayPoint{Label: d.Format("Mon"), Value: total}
	}
	return out
}

func buildSleepSeries(user users.User, logs []sleep.Log) []DayPoint {
	loc := user.Location()
	days := trailingDays(loc, sleepWindow)
	byDate := make(map[string]sleep.Log, len(logs))
	for _, l := range logs {
		byDate[l.LocalDate.In(loc).Format("2006-01-02")] = l
	}
	out := make([]DayPoint, len(days))
	for i, d := range days {
		hours := 0.0
		if row, ok := byDate[d.Format("2006-01-02")]; ok {
			hours = row.Hours()
		}
		out[i] = DayPoint{Label: d.Format("Mon"), Value: hours}
	}
	return out
}

func trailingDays(loc *time.Location, count int) []time.Time {
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	out := make([]time.Time, count)
	for i := range count {
		out[i] = today.AddDate(0, 0, -(count - 1 - i))
	}
	return out
}
