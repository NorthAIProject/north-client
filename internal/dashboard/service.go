package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/memories"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// goalCap is how many active goals the overview shows. The goals page has the
// rest; this page orients the day, it does not replace the list.
const goalCap = 3

// Options keeps the constructor readable as this page composes more slices.
type Options struct {
	CheckIns      *checkins.Service
	Goals         *goals.Service
	Conversations *conversations.Service
	Workouts      *workouts.Service
	Memories      *memories.Service
}

type Service struct {
	checkins      *checkins.Service
	goals         *goals.Service
	conversations *conversations.Service
	workouts      *workouts.Service
	memories      *memories.Service
}

func NewService(opts Options) *Service {
	return &Service{
		checkins:      opts.CheckIns,
		goals:         opts.Goals,
		conversations: opts.Conversations,
		workouts:      opts.Workouts,
		memories:      opts.Memories,
	}
}

// Snapshot is today's next actions for one person.
type Snapshot struct {
	CheckedInToday  bool
	Streak          int
	PendingMemories int
	Goals           []goals.Goal
	LastThread      *conversations.Conversation
	NextSession     *plan.PlanDay
	PlanID          uuid.UUID
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

	return snap, nil
}
