package crews

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// CheckInsFrom reads the board's check-in signals from the check-ins slice.
func CheckInsFrom(svc *checkins.Service) CheckInReader { return checkInSource{svc} }

type checkInSource struct{ svc *checkins.Service }

func (c checkInSource) CheckedInOn(ctx context.Context, userID uuid.UUID, day time.Time) (bool, error) {
	last, ok, err := c.svc.LatestLocalDate(ctx, userID)
	if err != nil || !ok {
		return false, err
	}
	return last.Year() == day.Year() && last.YearDay() == day.YearDay(), nil
}

func (c checkInSource) Streak(ctx context.Context, user users.User, now time.Time) (int, error) {
	return c.svc.StreakAt(ctx, user, now)
}

func (c checkInSource) CountBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) (int, error) {
	list, err := c.svc.ListBetween(ctx, userID, timerange.Between(from, to))
	return len(list), err
}

// WorkoutsFrom counts finished sessions from the activity slice: timed,
// logged, or imported.
func WorkoutsFrom(svc *activity.Service) WorkoutReader { return workoutSource{svc} }

type workoutSource struct{ svc *activity.Service }

func (w workoutSource) CountBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) (int, error) {
	list, err := w.svc.ListBetween(ctx, userID, timerange.Between(from, to))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range list {
		if s.EndedAt != nil {
			n++
		}
	}
	return n, nil
}
