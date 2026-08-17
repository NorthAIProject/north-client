package reports

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/NorthAIProject/north-client/internal/users"
)

// generateHour is the local hour from which the closed week may be written.
//
// Not midnight: a review of the week that just ended is something somebody
// reads over Monday morning coffee, and generating it at 00:00 local means the
// model runs against a week whose last check-in may be minutes old.
const generateHour = 6

// Sweeper generates the weekly review for accounts that opted in, once their
// own week has closed.
//
// Everything here turns on the user's timezone rather than the server's: a
// week does not end at the same instant in Auckland and Los Angeles, and a
// review that covers the wrong seven days is worse than a late one.
type Sweeper struct {
	svc      *Service
	accounts Accounts
	prefs    Prefs
	log      *slog.Logger
	now      func() time.Time
}

func NewSweeper(svc *Service, accounts Accounts, prefs Prefs, log *slog.Logger) *Sweeper {
	if log == nil {
		log = slog.Default()
	}
	return &Sweeper{svc: svc, accounts: accounts, prefs: prefs, log: log, now: time.Now}
}

// WithClock fixes now so tests do not have to wait for a Monday.
func (s *Sweeper) WithClock(now func() time.Time) *Sweeper {
	s.now = now
	return s
}

func (s *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if s.svc == nil || s.accounts == nil {
		return nil
	}
	return sweepAccounts(ctx, s.accounts, s.log, "weekly reviews", s.sweepUser)
}

// sweepUser generates the closed week for one account, if it is that account's
// Monday and they asked for this.
func (s *Sweeper) sweepUser(ctx context.Context, user users.User) (bool, error) {
	if s.prefs == nil {
		return false, nil
	}

	prefs, err := s.prefs.Get(ctx, user.ID)
	if err != nil {
		return false, err
	}
	if !prefs.WeeklyReportAuto {
		return false, nil
	}

	local, due := localTimeIfDue(s.now(), user, generateHour)
	if !due || local.Weekday() != time.Monday {
		return false, nil
	}

	loc := user.Location()
	closed := WeekContaining(local, loc)
	previous := WeekContaining(closed.Start.AddDate(0, 0, -1), loc)

	_, created, err := s.svc.EnsureWeekly(ctx, user.ID, previous)
	return created, err
}
