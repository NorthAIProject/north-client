package awards

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/sweep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// sweepHour is the local hour the safety net runs in.
const sweepHour = 4

// sweepWindow is how far back a log counts as "something happened here".
const sweepWindow = 24 * time.Hour

// Sweeper is the nightly safety net. Check at write time does the real work;
// this catches what arrived without a request of the person's own (a Strava
// sync, a Health push) by evaluating the areas they logged in during the last
// day. An area with no log in that window is never evaluated, so someone with
// years of history and nothing new earns nothing here.
type Sweeper struct {
	accounts sweep.Accounts
	svc      *Service
	log      *slog.Logger
	now      func() time.Time
}

func NewSweeper(accounts sweep.Accounts, svc *Service, log *slog.Logger) *Sweeper {
	if log == nil {
		log = slog.Default()
	}
	return &Sweeper{accounts: accounts, svc: svc, log: log, now: time.Now}
}

// WithClock fixes now, for tests.
func (s *Sweeper) WithClock(now func() time.Time) *Sweeper { s.now = now; return s }

// HandleSweep is the job handler. Runs hourly; it only works at 4 a.m. in
// each person's time zone.
func (s *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	now := s.now()
	return sweep.Run(ctx, s.accounts, s.log, "badges",
		func(ctx context.Context, user users.User) (bool, error) {
			return s.sweepOne(ctx, now, user)
		})
}

func (s *Sweeper) sweepOne(ctx context.Context, now time.Time, user users.User) (bool, error) {
	if now.In(user.Location()).Hour() != sweepHour {
		return false, nil
	}
	areas, err := s.svc.repo.ActiveAreas(ctx, user.ID, now.Add(-sweepWindow))
	if err != nil {
		return false, err
	}
	if len(areas) == 0 {
		return false, nil
	}
	earned, err := s.svc.Evaluate(ctx, user.ID, now, areas...)
	return len(earned) > 0, err
}
