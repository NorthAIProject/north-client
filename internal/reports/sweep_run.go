package reports

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/shared/sweep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The sweep loop moved to internal/shared/sweep when a third consumer — the
// insights digest — arrived. These stay as the names the two report sweeps
// already call, so that move touched no logic here.

// Accounts is the page of users a sweep walks.
type Accounts = sweep.Accounts

// Prefs answers whether this person asked for a thing to write itself.
type Prefs interface {
	Get(ctx context.Context, userID uuid.UUID) (notifications.Prefs, error)
}

func sweepAccounts(
	ctx context.Context,
	accounts Accounts,
	log *slog.Logger,
	what string,
	step func(ctx context.Context, user users.User) (bool, error),
) error {
	return sweep.Run(ctx, accounts, log, what, step)
}

func localTimeIfDue(now time.Time, user users.User, hour int) (time.Time, bool) {
	return sweep.LocalTimeIfDue(now, user, hour)
}
