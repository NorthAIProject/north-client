package insights

import (
	"context"

	"golang.org/x/sync/errgroup"

	activitysvc "github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// timelinePageSize is how many rows the full feed shows. Larger than the
// overview's dozen, still bounded: a year of logs is not a page.
const timelinePageSize = 200

// Options wires the slices these pages read.
//
// Dashboard is here rather than duplicated: the activity feed is already
// assembled there, correctly, and a second implementation would drift.
type Options struct {
	Dashboard *dashboard.Service
	CheckIns  *checkins.Service
	Hydration *hydration.Service
	Sleep     *sleep.Service
	Habits    *habits.Service
	Goals     *goals.Service
	Mind      *mind.Service
	Activity  *activitysvc.Service
}

type Service struct {
	dashboard *dashboard.Service
	checkins  *checkins.Service
	hydration *hydration.Service
	sleep     *sleep.Service
	habits    *habits.Service
	goals     *goals.Service
	mind      *mind.Service
	activity  *activitysvc.Service
}

func NewService(opts Options) *Service {
	return &Service{
		dashboard: opts.Dashboard,
		checkins:  opts.CheckIns,
		hydration: opts.Hydration,
		sleep:     opts.Sleep,
		habits:    opts.Habits,
		goals:     opts.Goals,
		mind:      opts.Mind,
		activity:  opts.Activity,
	}
}

// Timeline is the full activity feed for a window, optionally narrowed to one
// kind. Filtering happens here rather than in SQL because the feed is a merge
// across eight slices and there is no single query to push a predicate into.
func (s *Service) Timeline(ctx context.Context, user users.User, rg timerange.Range, kind string) (TimelineData, error) {
	feed, err := s.dashboard.Timeline(ctx, user, rg, timelinePageSize)
	if err != nil {
		return TimelineData{}, err
	}

	counts := make(map[dashboard.EntryKind]int, len(feed))
	for _, e := range feed {
		counts[e.Kind]++
	}

	shown := feed
	if kind != "" {
		shown = make([]dashboard.Entry, 0, counts[dashboard.EntryKind(kind)])
		for _, e := range feed {
			if string(e.Kind) == kind {
				shown = append(shown, e)
			}
		}
	}

	return TimelineData{
		Range:    rg,
		Kind:     kind,
		Entries:  shown,
		Counts:   counts,
		Total:    len(feed),
		Overflow: len(feed) == timelinePageSize,
	}, nil
}

// BodyData is hydration, sleep, and habits over a window.
type BodyData struct {
	Range timerange.Range

	Hydration     []hydration.Day
	HydrationGoal int

	Nights     []sleep.Log
	SleepTrend sleep.Trend

	Habits []habits.Stats
}

func (s *Service) Body(ctx context.Context, user users.User, rg timerange.Range) (BodyData, error) {
	out := BodyData{Range: rg, HydrationGoal: hydration.DefaultDailyTargetML}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() (err error) {
		out.Hydration, err = s.hydration.DaysBetween(gctx, user, rg)
		return
	})
	g.Go(func() (err error) {
		out.Nights, err = s.sleep.ListBetween(gctx, user, rg)
		return
	})
	g.Go(func() (err error) {
		out.SleepTrend, err = s.sleep.RecentTrend(gctx, user, rg.Days())
		return
	})
	g.Go(func() (err error) {
		out.Habits, err = s.habits.Today(gctx, user)
		return
	})

	if err := g.Wait(); err != nil {
		return BodyData{}, err
	}
	return out, nil
}

// MindData is mood, energy, and journalling over a window.
type MindData struct {
	Range timerange.Range

	CheckIns []checkins.CheckIn
	Journal  []mind.JournalEntry
}

func (s *Service) Mind(ctx context.Context, user users.User, rg timerange.Range) (MindData, error) {
	out := MindData{Range: rg}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() (err error) {
		out.CheckIns, err = s.checkins.ListBetween(gctx, user.ID, rg)
		return
	})
	g.Go(func() (err error) {
		out.Journal, err = s.mind.ListBetween(gctx, user.ID, rg)
		return
	})

	if err := g.Wait(); err != nil {
		return MindData{}, err
	}
	return out, nil
}

// ProgressData is goals, their notes, and their milestones over a window.
type ProgressData struct {
	Range timerange.Range

	Active   []goals.Goal
	Opened   []goals.Goal
	Notes    []goals.TimelineUpdate
	Overdue  int
	Streak   int
	CheckIns []checkins.CheckIn
}

func (s *Service) Progress(ctx context.Context, user users.User, rg timerange.Range) (ProgressData, error) {
	out := ProgressData{Range: rg}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() (err error) {
		out.Active, err = s.goals.List(gctx, user.ID)
		return
	})
	g.Go(func() (err error) {
		out.Opened, err = s.goals.CreatedBetween(gctx, user.ID, rg)
		return
	})
	g.Go(func() (err error) {
		out.Notes, err = s.goals.UpdatesBetween(gctx, user.ID, rg)
		return
	})
	g.Go(func() (err error) {
		out.Overdue, err = s.goals.CountOverdueMilestones(gctx, user.ID)
		return
	})
	g.Go(func() (err error) {
		out.Streak, err = s.checkins.Streak(gctx, user)
		return
	})
	g.Go(func() (err error) {
		out.CheckIns, err = s.checkins.ListBetween(gctx, user.ID, rg)
		return
	})

	if err := g.Wait(); err != nil {
		return ProgressData{}, err
	}
	return out, nil
}

// TrainingData is activity sessions and their calorie burn over a window.
type TrainingData struct {
	Range timerange.Range

	Sessions []activity.Session
	Calories float64
	Prior    float64
}

func (s *Service) Training(ctx context.Context, user users.User, rg timerange.Range) (TrainingData, error) {
	out := TrainingData{Range: rg}
	prev := rg.Previous()

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() (err error) {
		out.Sessions, err = s.activity.ListBetween(gctx, user.ID, rg)
		return
	})
	g.Go(func() (err error) {
		out.Calories, err = s.activity.CaloriesBetween(gctx, user.ID, rg)
		return
	})
	g.Go(func() (err error) {
		out.Prior, err = s.activity.CaloriesBetween(gctx, user.ID, prev)
		return
	})

	if err := g.Wait(); err != nil {
		return TrainingData{}, err
	}
	return out, nil
}

// TimelineData is the full feed page.
type TimelineData struct {
	Range   timerange.Range
	Kind    string
	Entries []dashboard.Entry
	Counts  map[dashboard.EntryKind]int
	Total   int

	// Overflow marks a window with more rows than one page holds, so the
	// template can say so rather than quietly truncating.
	Overflow bool
}
