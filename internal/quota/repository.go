package quota

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	quotadb "github.com/NorthAIProject/north-client/internal/quota/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Repository counts in Postgres. It satisfies Counter.
type Repository struct {
	q *quotadb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: quotadb.New(pool)}
}

// Consume increments this account's usage of an action and returns the running
// total for the window the database placed it in.
//
// No transaction: the increment is a single statement whose conflict clause
// does the work under the row lock, so there is nothing for a transaction to
// make atomic that is not already atomic.
func (r *Repository) Consume(ctx context.Context, userID uuid.UUID, action Action, window time.Duration) (Count, error) {
	row, err := r.q.ConsumeQuota(ctx, quotadb.ConsumeQuotaParams{
		UserID: userID,
		Action: string(action),
		// Seconds rather than the duration: the query divides the epoch by this
		// to find the window floor, and Postgres has no interval division that
		// would read more clearly.
		WindowSeconds: int64(window / time.Second),
	})
	if err != nil {
		return Count{}, apperr.Wrap(err, "consume quota for action %q", action)
	}

	return Count{Used: int(row.Used), WindowStart: row.WindowStart}, nil
}

// Sweep drops windows that have already closed.
func (r *Repository) Sweep(ctx context.Context, before time.Time) error {
	if err := r.q.SweepQuotaCounters(ctx, before); err != nil {
		return apperr.Wrap(err, "sweep quota counters")
	}
	return nil
}
