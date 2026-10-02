package insights

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/sweep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// AreaStreakWeeks is how many weeks in a row an area must be on track for
// the achievement: a month of it, which is a habit rather than a good week.
const AreaStreakWeeks = 4

// areaStreakFrom is "on track", the same line the summary's "n of m on
// track" draws.
const areaStreakFrom = onTrackFrom

// The Monday hours the sweep looks in. A few, so a worker restart in one
// hour does not skip the week; the achievement's own key stops a second award.
const (
	areaStreakFirstHour = 7
	areaStreakLastHour  = 9
)

// AreaAchievements records a streak. achievements.Service satisfies it.
type AreaAchievements interface {
	AreaStreak(ctx context.Context, userID uuid.UUID, area, label string, weeks int, runStart, at time.Time)
}

// AreaStreakSweeper looks, on Monday morning, for areas that have just been
// on track AreaStreakWeeks weeks running.
type AreaStreakSweeper struct {
	accounts     sweep.Accounts
	svc          *Service
	achievements AreaAchievements
	log          *slog.Logger
	now          func() time.Time
}

func NewAreaStreakSweeper(accounts sweep.Accounts, svc *Service, a AreaAchievements, log *slog.Logger) *AreaStreakSweeper {
	if log == nil {
		log = slog.Default()
	}
	return &AreaStreakSweeper{accounts: accounts, svc: svc, achievements: a, log: log, now: time.Now}
}

// HandleSweep is the job handler. Runs hourly; it only works on Monday
// mornings in each person's time zone.
func (s *AreaStreakSweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	now := s.now()
	return sweep.Run(ctx, s.accounts, s.log, "area streaks",
		func(ctx context.Context, user users.User) (bool, error) {
			return s.sweepOne(ctx, now, user)
		})
}

func (s *AreaStreakSweeper) sweepOne(ctx context.Context, now time.Time, user users.User) (bool, error) {
	local := now.In(user.Location())
	if local.Weekday() != time.Monday || local.Hour() < areaStreakFirstHour || local.Hour() > areaStreakLastHour {
		return false, nil
	}
	// One more complete week than the streak, to tell a run that has just
	// reached it from one that reached it earlier, plus the week just begun.
	areas, err := s.svc.AreaTrend(ctx, user, AreaStreakWeeks+2, now)
	if err != nil {
		return false, err
	}
	awarded := false
	for _, a := range areas.Areas {
		if start, ok := streakJustReached(a.Trend, areas.Weeks); ok {
			s.achievements.AreaStreak(ctx, user.ID, a.Key, a.Label, AreaStreakWeeks, start, now)
			awarded = true
		}
	}
	return awarded, nil
}

// streakJustReached reports whether the complete weeks end in a run of
// exactly AreaStreakWeeks on-track weeks, and the Monday it started. The last
// point is the week in progress and is not counted.
func streakJustReached(trend []AreaPoint, mondays []time.Time) (time.Time, bool) {
	complete := len(trend) - 1
	if complete < AreaStreakWeeks+1 || len(mondays) != len(trend) {
		return time.Time{}, false
	}
	run := 0
	for i := complete - 1; i >= 0; i-- {
		if !trend[i].HasData || trend[i].Points < areaStreakFrom {
			break
		}
		run++
	}
	if run != AreaStreakWeeks {
		return time.Time{}, false
	}
	return mondays[complete-AreaStreakWeeks], true
}
