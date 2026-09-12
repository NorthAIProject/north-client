// Package sweep walks every onboarded account and does one thing per person.
//
// Every periodic job in the application needs exactly this: page through
// accounts, act on each, keep going when one of them fails, and say at the end
// how many were started. It was written once inside the reports slice with a
// comment saying the second copy of it had been a copy; this package exists
// because a third consumer arrived and the honest answer to that comment was
// to move it rather than to copy it again.
package sweep

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/users"
)

// PageSize is how many accounts a sweep reads at a time.
const PageSize = 100

// Accounts is the page of users a sweep walks.
type Accounts interface {
	ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error)
}

// Run applies step to every onboarded account.
//
// step reports whether it started work for that account. A step that returns
// an error is logged and skipped: one person's broken row must not stop the
// sweep reaching everybody after them in the page.
func Run(
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

		page, err := accounts.ListOnboarded(ctx, after, PageSize)
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

		if len(page) < PageSize {
			break
		}
	}

	if started > 0 {
		log.Info("sweep started "+what, slog.Int("started", started))
	}
	return nil
}

// LocalTimeIfDue returns the account's local time, and whether it has reached
// the hour from which this work may run.
//
// The one piece of reasoning every sweep shares and none may get wrong: the
// hour is the reader's, not the server's. A worker in UTC deciding it is 6am
// would write somebody in Auckland their morning briefing at their 6pm.
//
// From the hour onwards rather than exactly at it, because a worker that was
// down for that single hour would otherwise skip the day entirely. Repeating
// must therefore be free — every caller needs something that makes a second
// run a no-op, whether that is an idempotent Ensure or a claim ledger.
func LocalTimeIfDue(now time.Time, user users.User, hour int) (time.Time, bool) {
	local := now.In(user.Location())
	return local, local.Hour() >= hour
}
