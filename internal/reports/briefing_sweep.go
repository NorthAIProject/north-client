package reports

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/users"
)

// briefingHour is the local hour from which the morning briefing may be
// written.
//
// Earlier than the weekly review's generateHour because this one is meant to be
// waiting when somebody wakes up, and a briefing generated at 05:00 covers the
// same recorded day as one generated at 06:00 — nothing is logged overnight.
const briefingHour = 5

// BriefingSweeper writes the morning briefing for accounts that opted in, once
// their own morning has arrived.
//
// A separate type from Sweeper rather than a mode on it: the two share only the
// paging loop, and folding them together would mean one opt-in flag, one clock
// gate, and one log line trying to describe two different products.
type BriefingSweeper struct {
	svc      *Service
	accounts Accounts
	prefs    Prefs
	log      *slog.Logger
	now      func() time.Time
}

func NewBriefingSweeper(svc *Service, accounts Accounts, prefs Prefs, log *slog.Logger) *BriefingSweeper {
	if log == nil {
		log = slog.Default()
	}
	return &BriefingSweeper{svc: svc, accounts: accounts, prefs: prefs, log: log, now: time.Now}
}

// WithClock fixes now so tests do not have to wait for morning.
func (s *BriefingSweeper) WithClock(now func() time.Time) *BriefingSweeper {
	s.now = now
	return s
}

func (s *BriefingSweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
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
				s.log.Error("daily briefing sweep failed",
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
		s.log.Info("sweep started daily briefings", slog.Int("started", started))
	}
	return nil
}

// sweepUser writes today's briefing for one account, if their morning has come
// and they asked for this.
func (s *BriefingSweeper) sweepUser(ctx context.Context, user users.User) (bool, error) {
	if s.prefs == nil {
		return false, nil
	}

	prefs, err := s.prefs.Get(ctx, user.ID)
	if err != nil {
		return false, err
	}
	if !prefs.DailyBriefingAuto {
		return false, nil
	}

	loc := user.Location()
	local := s.now().In(loc)
	if local.Hour() < briefingHour {
		return false, nil
	}

	// Today's briefing, not yesterday's: unlike the weekly review this is a
	// forward-looking note about the day starting, and it reads the day that
	// just ended as context. From briefingHour onwards rather than exactly at
	// it, so an hour of worker downtime does not cost somebody their morning —
	// EnsureBriefing does nothing once the day has a row.
	_, created, err := s.svc.EnsureBriefing(ctx, user.ID, DayContaining(local, loc))
	return created, err
}
