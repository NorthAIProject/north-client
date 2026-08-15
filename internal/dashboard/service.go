package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/nudges/nudge"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

const (
	goalCap = 3

	// timelineCap is how many rows the overview's feed shows before it defers
	// to the full page. Deep history belongs on /app/insights/timeline.
	timelineCap = 12

	// minChartDays keeps a trend line from collapsing to a single point when
	// the reader selects Today. The tiles report the window they asked for;
	// the charts widen so there is a shape to read.
	minChartDays = 7

	// minCheckInDays is the check-in series' own floor. Mood and energy are one
	// point per day, and a fortnight is the shortest window in which a run of
	// bad days reads as a run rather than as noise — which is the whole reason
	// the series is on the page. Hydration keeps the shorter default: it is a
	// daily target, not a trend.
	minCheckInDays = 14
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
	Mind          *mind.Service

	// Nudges is optional. The worker process never loads the dashboard, and
	// a nil here just leaves the card off.
	Nudges Nudges
}

// Nudges is the dashboard's view of scheduled coach notes.
type Nudges interface {
	ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]nudge.Nudge, error)
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
	mind          *mind.Service
	nudges        Nudges
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
		mind:          opts.Mind,
		nudges:        opts.Nudges,
	}
}

// Snapshot is the command center for one person over one window.
type Snapshot struct {
	// Range is the window the reader selected. Charts may cover more (see
	// minChartDays); the tiles and the feed cover exactly this.
	Range timerange.Range

	// Today's operational state. Deliberately not range-scoped: "have I
	// checked in" is a question about now, whichever window is on screen.
	CheckedInToday  bool
	Streak          int
	PendingMemories int
	Goals           []goals.Goal
	LastThread      *conversations.Conversation
	NextSession     *plan.PlanDay
	PlanID          uuid.UUID

	// Scoped to Range.
	CheckIns         CheckInSeries
	Habits           HabitsSummary
	Hydration        HydrationSummary
	Sleep            SleepSummary
	ActivityCalories float64
	Timeline         []Entry
	Deltas           Deltas

	// Unread coach notes. Empty when none, or when Nudges was not wired.
	Nudges []nudge.Nudge
}

// Load gathers the overview.
//
// The slices are queried concurrently because this page now touches a dozen of
// them and doing that in series is a visible wait. The error policy is
// unchanged from when it was sequential: a missing row is an empty section, a
// real error fails the page rather than showing a half-truth.
func (s *Service) Load(ctx context.Context, user users.User, rg timerange.Range) (Snapshot, error) {
	snap := Snapshot{Range: rg}
	chartRg := widen(rg, minChartDays)
	prev := rg.Previous()

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		_, err := s.checkins.Today(gctx, user)
		switch {
		case err == nil:
			snap.CheckedInToday = true
			return nil
		case apperr.Is(err, apperr.ErrNotFound):
			return nil
		default:
			return err
		}
	})

	g.Go(func() error {
		streak, err := s.checkins.Streak(gctx, user)
		snap.Streak = streak
		return err
	})

	g.Go(func() error {
		pending, err := s.memories.CountPending(gctx, user.ID)
		snap.PendingMemories = pending
		return err
	})

	if s.nudges != nil {
		g.Go(func() error {
			list, err := s.nudges.ListOpen(gctx, user.ID, 10)
			if err != nil {
				return err
			}
			for _, n := range list {
				if n.Unread() {
					snap.Nudges = append(snap.Nudges, n)
				}
			}
			return nil
		})
	}

	g.Go(func() error {
		active, err := s.goals.ListActive(gctx, user.ID)
		if err != nil {
			return err
		}
		if len(active) > goalCap {
			active = active[:goalCap]
		}
		snap.Goals = active
		return nil
	})

	g.Go(func() error {
		threads, err := s.conversations.List(gctx, user.ID, 1)
		if err != nil {
			return err
		}
		if len(threads) > 0 {
			c := threads[0]
			snap.LastThread = &c
		}
		return nil
	})

	g.Go(func() error {
		stored, err := s.workouts.LatestPlan(gctx, user.ID)
		switch {
		case err == nil:
			snap.PlanID = stored.ID
			if day, ok := stored.Plan.NextSession(time.Now().In(user.Location())); ok {
				snap.NextSession = &day
			}
			return nil
		case apperr.Is(err, apperr.ErrNotFound):
			return nil
		default:
			return err
		}
	})

	g.Go(func() error {
		// Its own window, wider than the shared chart one. The delta below
		// still counts within rg, so the tile reports the period the reader
		// asked for even though the series behind it reaches further back.
		checkInRg := widen(rg, minCheckInDays)

		list, err := s.checkins.ListBetween(gctx, user.ID, checkInRg)
		if err != nil {
			return err
		}
		snap.CheckIns = buildCheckInSeries(checkInRg, list)
		snap.Deltas.CheckIns = computeDelta(float64(countIn(rg, list)), 0)
		return nil
	})

	g.Go(func() error {
		if s.habits == nil {
			return nil
		}
		stats, err := s.habits.Today(gctx, user)
		if err != nil {
			return err
		}
		snap.Habits = summarizeHabits(stats)
		return nil
	})

	g.Go(func() error {
		if s.hydration == nil {
			return nil
		}
		today, err := s.hydration.Today(gctx, user)
		if err != nil {
			return err
		}
		days, err := s.hydration.DaysBetween(gctx, user, chartRg)
		if err != nil {
			return err
		}
		snap.Hydration = buildHydrationSummary(chartRg, today, days)

		priorDays, err := s.hydration.DaysBetween(gctx, user, prev)
		if err != nil {
			return err
		}
		snap.Deltas.Hydration = computeDelta(sumML(days, rg), sumML(priorDays, prev))
		return nil
	})

	g.Go(func() error {
		if s.sleep == nil {
			return nil
		}
		log, ok, err := s.sleep.Today(gctx, user)
		if err != nil {
			return err
		}
		if ok {
			snap.Sleep = SleepSummary{
				Logged:          true,
				DurationMinutes: log.DurationMinutes,
				Quality:         log.Quality,
			}
		}

		nights, err := s.sleep.ListBetween(gctx, user, rg)
		if err != nil {
			return err
		}
		priorNights, err := s.sleep.ListBetween(gctx, user, prev)
		if err != nil {
			return err
		}
		snap.Deltas.SleepHours = computeDelta(averageHours(nights), averageHours(priorNights))
		return nil
	})

	g.Go(func() error {
		if s.activity == nil {
			return nil
		}
		total, err := s.activity.CaloriesBetween(gctx, user.ID, rg)
		if err != nil {
			return err
		}
		prior, err := s.activity.CaloriesBetween(gctx, user.ID, prev)
		if err != nil {
			return err
		}
		snap.ActivityCalories = total
		snap.Deltas.Calories = computeDelta(total, prior)
		return nil
	})

	g.Go(func() error {
		feed, err := s.Timeline(gctx, user, rg, timelineCap)
		if err != nil {
			return err
		}
		snap.Timeline = feed
		return nil
	})

	if err := g.Wait(); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// widen grows a range to at least min days, keeping its end fixed. Used for
// charts only: a one-day window makes a line chart with a single point.
func widen(rg timerange.Range, min int) timerange.Range {
	if rg.Days() >= min {
		return rg
	}
	out := rg
	out.Since = rg.Until.AddDate(0, 0, -min)

	// The grain has to coarsen with the window. A single day is charted hour by
	// hour, and stretching that to a week without saying so leaves 168 hourly
	// buckets — which the day-keyed series builders then fill by date, drawing
	// each day's value twenty-four times over.
	out.Grain = timerange.GrainDay

	return out
}

func countIn(rg timerange.Range, list []checkins.CheckIn) int {
	n := 0
	for _, c := range list {
		if rg.Contains(c.LocalDate) {
			n++
		}
	}
	return n
}

func sumML(days []hydration.Day, rg timerange.Range) float64 {
	total := 0
	for _, d := range days {
		if rg.Contains(d.Date) {
			total += d.TotalML
		}
	}
	return float64(total)
}

// averageHours averages over nights recorded rather than nights elapsed, the
// same way sleep.RecentTrend does: someone who logged three of seven has a
// 6.5h average across three nights, not across seven.
func averageHours(nights []sleep.Log) float64 {
	if len(nights) == 0 {
		return 0
	}
	total := 0
	for _, n := range nights {
		total += n.DurationMinutes
	}
	return float64(total) / float64(len(nights)) / 60
}

func buildCheckInSeries(rg timerange.Range, list []checkins.CheckIn) CheckInSeries {
	loc := rg.Location()
	buckets := rg.Buckets()

	byDate := make(map[string]checkins.CheckIn, len(list))
	for _, c := range list {
		byDate[c.LocalDate.In(loc).Format("2006-01-02")] = c
	}

	out := make([]CheckInDay, len(buckets))
	for i, b := range buckets {
		day := CheckInDay{Label: b.Label}
		if c, ok := byDate[b.Start.Format("2006-01-02")]; ok {
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

func buildHydrationSummary(rg timerange.Range, today hydration.Day, recent []hydration.Day) HydrationSummary {
	loc := rg.Location()
	buckets := rg.Buckets()

	byDate := make(map[string]hydration.Day, len(recent))
	for _, d := range recent {
		byDate[d.Date.In(loc).Format("2006-01-02")] = d
	}

	target := today.TargetML
	if target <= 0 {
		target = hydration.DefaultDailyTargetML
	}

	out := make([]HydrationDay, len(buckets))
	for i, b := range buckets {
		dayTarget := target
		total := 0
		if row, ok := byDate[b.Start.Format("2006-01-02")]; ok {
			total = row.TotalML
			if row.TargetML > 0 {
				dayTarget = row.TargetML
			}
		}
		out[i] = HydrationDay{
			Label:    b.Label,
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
