package reports

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/users"
)

const sweepPageSize = 100

// generateHour is the local hour from which the closed week may be written.
//
// Not midnight: a review of the week that just ended is something somebody
// reads over Monday morning coffee, and generating it at 00:00 local means the
// model runs against a week whose last check-in may be minutes old.
const generateHour = 6

// Accounts is the page of users the sweep walks.
type Accounts interface {
	ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error)
}

// Prefs answers whether this person asked for the weekly review to write
// itself.
type Prefs interface {
	Get(ctx context.Context, userID uuid.UUID) (notifications.Prefs, error)
}

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

	var after uuid.UUID
	started := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		page, err := s.accounts.ListOnboarded(ctx, after, sweepPageSize)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}

		for _, user := range page {
			after = user.ID

			ok, err := s.sweepUser(ctx, user)
			if err != nil {
				s.log.Error("weekly review sweep failed",
					slog.String("user_id", user.ID.String()),
					slog.Any("error", err),
				)
				continue
			}
			if ok {
				started++
			}
		}

		if len(page) < sweepPageSize {
			break
		}
	}

	if started > 0 {
		s.log.Info("sweep started weekly reviews", slog.Int("started", started))
	}
	return nil
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

	loc := user.Location()
	local := s.now().In(loc)
	if local.Weekday() != time.Monday || local.Hour() < generateHour {
		return false, nil
	}

	// From generateHour onwards rather than exactly at it: a worker that was
	// down for that one hour would otherwise skip somebody's week entirely.
	// Repeating is free — EnsureWeekly does nothing once the week has a row.
	closed := WeekContaining(local, loc)
	previous := WeekContaining(closed.Start.AddDate(0, 0, -1), loc)

	_, created, err := s.svc.EnsureWeekly(ctx, user.ID, previous)
	return created, err
}
