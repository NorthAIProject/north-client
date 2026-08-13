package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/memories"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

const (
	goalCap       = 3
	checkInWindow = 14
	hydrationBars = 7
)

// Options keeps the constructor readable as this page composes more slices.
type Options struct {
	CheckIns      *checkins.Service
	Goals         *goals.Service
	Conversations *conversations.Service
	Workouts      *workouts.Service
	Memories      *memories.Service
	Habits        *habits.Service
	Hydration     *hydration.Service
	Sleep         *sleep.Service
	Activity      *activity.Service
}

type Service struct {
	checkins      *checkins.Service
	goals         *goals.Service
	conversations *conversations.Service
	workouts      *workouts.Service
	memories      *memories.Service
	habits        *habits.Service
	hydration     *hydration.Service
	sleep         *sleep.Service
	activity      *activity.Service
}

func NewService(opts Options) *Service {
	return &Service{
		checkins:      opts.CheckIns,
		goals:         opts.Goals,
		conversations: opts.Conversations,
		workouts:      opts.Workouts,
		memories:      opts.Memories,
		habits:        opts.Habits,
		hydration:     opts.Hydration,
		sleep:         opts.Sleep,
		activity:      opts.Activity,
	}
}

// Snapshot is today's command-center data for one person.
type Snapshot struct {
	CheckedInToday     bool
	Streak             int
	PendingMemories    int
	Goals              []goals.Goal
	LastThread         *conversations.Conversation
	NextSession        *plan.PlanDay
	PlanID             uuid.UUID
	CheckIns           CheckInSeries
	Habits             HabitsSummary
	Hydration          HydrationSummary
	Sleep              SleepSummary
	ActivityCalories7d float64
}

// Load gathers the overview. Missing rows are empty sections; a real error
// fails the page rather than showing a half-truth.
func (s *Service) Load(ctx context.Context, user users.User) (Snapshot, error) {
	var snap Snapshot

	_, err := s.checkins.Today(ctx, user)
	switch {
	case err == nil:
		snap.CheckedInToday = true
	case !apperr.Is(err, apperr.ErrNotFound):
		return Snapshot{}, err
	}

	streak, err := s.checkins.Streak(ctx, user)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Streak = streak

	pending, err := s.memories.CountPending(ctx, user.ID)
	if err != nil {
		return Snapshot{}, err
	}
	snap.PendingMemories = pending

	active, err := s.goals.ListActive(ctx, user.ID)
	if err != nil {
		return Snapshot{}, err
	}
	if len(active) > goalCap {
		active = active[:goalCap]
	}
	snap.Goals = active

	threads, err := s.conversations.List(ctx, user.ID, 1)
	if err != nil {
		return Snapshot{}, err
	}
	if len(threads) > 0 {
		c := threads[0]
		snap.LastThread = &c
	}

	stored, err := s.workouts.LatestPlan(ctx, user.ID)
	switch {
	case err == nil:
		snap.PlanID = stored.ID
		if day, ok := stored.Plan.NextSession(time.Now().In(user.Location())); ok {
			snap.NextSession = &day
		}
	case !apperr.Is(err, apperr.ErrNotFound):
		return Snapshot{}, err
	}

	checkInList, err := s.checkins.List(ctx, user.ID, checkInWindow)
	if err != nil {
		return Snapshot{}, err
	}
	snap.CheckIns = buildCheckInSeries(user, checkInList)

	if s.habits != nil {
		stats, err := s.habits.Today(ctx, user)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Habits = summarizeHabits(stats)
	}

	if s.hydration != nil {
		today, err := s.hydration.Today(ctx, user)
		if err != nil {
			return Snapshot{}, err
		}
		recent, err := s.hydration.RecentDays(ctx, user, hydrationBars)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Hydration = buildHydrationSummary(user, today, recent)
	}

	if s.sleep != nil {
		log, ok, err := s.sleep.Today(ctx, user)
		if err != nil {
			return Snapshot{}, err
		}
		if ok {
			snap.Sleep = SleepSummary{
				Logged:          true,
				DurationMinutes: log.DurationMinutes,
				Quality:         log.Quality,
			}
		}
	}

	if s.activity != nil {
		loc := user.Location()
		now := time.Now().In(loc)
		since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -6)
		total, err := s.activity.TotalCaloriesSince(ctx, user.ID, since)
		if err != nil {
			return Snapshot{}, err
		}
		snap.ActivityCalories7d = total
	}

	return snap, nil
}

func buildCheckInSeries(user users.User, list []checkins.CheckIn) CheckInSeries {
	loc := user.Location()
	days := trailingCalendarDays(loc, checkInWindow)
	byDate := make(map[string]checkins.CheckIn, len(list))
	for _, c := range list {
		key := c.LocalDate.In(loc).Format("2006-01-02")
		byDate[key] = c
	}

	out := make([]CheckInDay, len(days))
	for i, d := range days {
		key := d.Format("2006-01-02")
		day := CheckInDay{Label: d.Format("2 Jan")}
		if c, ok := byDate[key]; ok {
			day.Mood = c.Mood
			day.Energy = c.Energy
		}
		out[i] = day
	}
	return CheckInSeries{Days: out}
}

func summarizeHabits(stats []habits.Stats) HabitsSummary {
	if len(stats) == 0 {
		return HabitsSummary{}
	}
	var kept, scheduled, best int
	for _, st := range stats {
		kept += st.Kept
		scheduled += st.Scheduled
		if st.Streak > best {
			best = st.Streak
		}
	}
	rate := 100
	if scheduled > 0 {
		rate = kept * 100 / scheduled
	}
	return HabitsSummary{
		HasHabits:  true,
		Rate:       rate,
		Kept:       kept,
		Scheduled:  scheduled,
		BestStreak: best,
	}
}

func buildHydrationSummary(user users.User, today hydration.Day, recent []hydration.Day) HydrationSummary {
	loc := user.Location()
	days := trailingCalendarDays(loc, hydrationBars)
	byDate := make(map[string]hydration.Day, len(recent))
	for _, d := range recent {
		key := d.Date.In(loc).Format("2006-01-02")
		byDate[key] = d
	}

	target := today.TargetML
	if target <= 0 {
		target = hydration.DefaultDailyTargetML
	}

	out := make([]HydrationDay, len(days))
	for i, d := range days {
		key := d.Format("2006-01-02")
		dayTarget := target
		total := 0
		if row, ok := byDate[key]; ok {
			total = row.TotalML
			if row.TargetML > 0 {
				dayTarget = row.TargetML
			}
		}
		out[i] = HydrationDay{
			Label:    d.Format("Mon"),
			TotalML:  total,
			TargetML: dayTarget,
		}
	}

	pct := 0
	if today.TargetML > 0 {
		pct = today.Percent()
	} else if target > 0 {
		pct = today.TotalML * 100 / target
		if pct > 100 {
			pct = 100
		}
	}

	return HydrationSummary{
		TodayML:  today.TotalML,
		TargetML: target,
		Percent:  pct,
		Days:     out,
	}
}

func trailingCalendarDays(loc *time.Location, count int) []time.Time {
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	out := make([]time.Time, count)
	for i := range count {
		out[i] = today.AddDate(0, 0, -(count - 1 - i))
	}
	return out
}
