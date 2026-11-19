package horizon

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/sweep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// sweepHour is the local hour forecasts are recorded in, on Mondays: before
// the week's first session, so each one is of a week not yet begun.
const sweepHour = 5

// Sweeper records each person's forecasts once a week, so that weeks later
// they can be compared with what happened. See snapshots.go.
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

// HandleSweep is the job handler. Runs hourly; it only works at 5 a.m. on
// Monday in each person's time zone. A run that is missed is not made up:
// a forecast recorded late would be of a week already under way.
func (s *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	now := s.now()
	return sweep.Run(ctx, s.accounts, s.log, "horizon",
		func(ctx context.Context, user users.User) (bool, error) {
			local := now.In(user.Location())
			if local.Weekday() != time.Monday || local.Hour() != sweepHour {
				return false, nil
			}
			return s.svc.RecordSnapshots(ctx, user)
		})
}
