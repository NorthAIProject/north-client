package reports

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/users"
)

const sweepPageSize = 100

// Accounts is the page of users a sweep walks.
type Accounts interface {
	ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error)
}

// Prefs answers whether this person asked for a thing to write itself.
type Prefs interface {
	Get(ctx context.Context, userID uuid.UUID) (notifications.Prefs, error)
}

// sweepAccounts walks every onboarded account and applies step to each.
//
// Both report sweeps need exactly this: page through accounts, do one thing per
// person, keep going when an individual fails, and say at the end how many were
// started. Written once because the second copy of it was a copy — the weekly
// review's loop and the briefing's differed only in a log string, which is the
// kind of duplication that stays identical right up until it silently does not.
//
// step reports whether it started work for that account. A step that returns an
// error is logged and skipped: one person's broken row must not stop the sweep
// reaching everybody after them in the page.
func sweepAccounts(
	ctx context.Context,
	accounts Accounts,
	log *slog.Logger,
	what string,
	step func(ctx context.Context, user users.User) (bool, error),
) error {
	var after uuid.UUID
	started := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		page, err := accounts.ListOnboarded(ctx, after, sweepPageSize)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}

		for _, user := range page {
			after = user.ID

			ok, err := step(ctx, user)
			if err != nil {
				log.Error(what+" sweep failed",
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
		log.Info("sweep started "+what, slog.Int("started", started))
	}
	return nil
}

// localTimeIfDue returns the account's local time, and whether it has reached
// the hour from which this work may run.
//
// The one piece of reasoning both sweeps share and neither may get wrong: the
// hour is the reader's, not the server's. A worker in UTC deciding it is 6am
// would write somebody in Auckland their morning briefing at their 6pm.
//
// From the hour onwards rather than exactly at it, because a worker that was
// down for that single hour would otherwise skip the day entirely. Repeating is
// free: the Ensure* methods do nothing once the period has a row.
func localTimeIfDue(now time.Time, user users.User, hour int) (time.Time, bool) {
	local := now.In(user.Location())
	return local, local.Hour() >= hour
}
