package crews

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/shared/sweep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The Monday hours the sweep closes a week in, in each person's zone. A few,
// so a worker restart in one hour does not skip the week; the achievement's
// own key stops a second record.
const (
	closeWeekFirstHour = 7
	closeWeekLastHour  = 9
)

// ChallengeAchievements records a challenge met. achievements.Service
// satisfies it.
type ChallengeAchievements interface {
	CrewChallengeMet(ctx context.Context, userID, crewID uuid.UUID, crewName, kind string, target int, weekStart, at time.Time)
}

// ChallengeSweeper closes each crew's week on Monday morning: every member
// who reached the challenge's target in the week just ended gets it recorded,
// which is what pays them XP.
type ChallengeSweeper struct {
	accounts     sweep.Accounts
	svc          *Service
	achievements ChallengeAchievements
	awards       Awards
	log          *slog.Logger
	now          func() time.Time
}

func NewChallengeSweeper(accounts sweep.Accounts, svc *Service, a ChallengeAchievements, log *slog.Logger) *ChallengeSweeper {
	if log == nil {
		log = slog.Default()
	}
	return &ChallengeSweeper{accounts: accounts, svc: svc, achievements: a, log: log, now: time.Now}
}

// Awards checks a person's badges after a write. awards.Service satisfies it.
type Awards interface {
	Check(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area)
}

func (s *ChallengeSweeper) WithAwards(a Awards) *ChallengeSweeper { s.awards = a; return s }

// WithClock fixes now, for tests.
func (s *ChallengeSweeper) WithClock(now func() time.Time) *ChallengeSweeper { s.now = now; return s }

// HandleSweep is the job handler. Runs hourly; it only works on Monday
// mornings in each person's time zone.
func (s *ChallengeSweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	now := s.now()
	return sweep.Run(ctx, s.accounts, s.log, "crew challenges",
		func(ctx context.Context, user users.User) (bool, error) {
			return s.sweepOne(ctx, now, user)
		})
}

func (s *ChallengeSweeper) sweepOne(ctx context.Context, now time.Time, user users.User) (bool, error) {
	local := now.In(user.Location())
	if local.Weekday() != time.Monday || local.Hour() < closeWeekFirstHour || local.Hour() > closeWeekLastHour {
		return false, nil
	}
	to := weekStart(local)
	from := to.AddDate(0, 0, -7)

	rows, err := s.svc.repo.ChallengesOf(ctx, user.ID)
	if err != nil {
		return false, err
	}
	recorded := false
	for _, r := range rows {
		// A challenge set after the week ended was not that week's: the
		// member could not have been aiming at it.
		if !r.CreatedAt.Before(to) {
			continue
		}
		done, err := s.svc.progress(ctx, user.ID, r.Kind, from, to)
		if err != nil {
			return recorded, err
		}
		if done >= r.Target {
			s.achievements.CrewChallengeMet(ctx, user.ID, r.CrewID, r.CrewName, r.Kind, r.Target, from, now)
			recorded = true
			if s.awards != nil {
				s.awards.Check(ctx, user.ID, now, award.Social)
			}
		}
	}
	return recorded, nil
}

// progress counts what a challenge of kind counts, for one person, between
// two instants.
func (s *Service) progress(ctx context.Context, userID uuid.UUID, kind string, from, to time.Time) (int, error) {
	switch {
	case kind == crew.ChallengeCheckIns && s.checkIns != nil:
		return s.checkIns.CountBetween(ctx, userID, from, to)
	case kind == crew.ChallengeWorkouts && s.workouts != nil:
		return s.workouts.CountBetween(ctx, userID, from, to)
	}
	return 0, nil
}
