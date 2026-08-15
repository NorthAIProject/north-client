package health

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	healthdb "github.com/NorthAIProject/north-client/internal/health/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *healthdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, q: healthdb.New(pool)}
}

// Save writes a whole payload in one transaction.
//
// The transaction is what makes a partial sync impossible. Validation rejects a
// malformed batch before it gets here, but a connection lost halfway through
// ten thousand readings would otherwise leave a gap that looks exactly like a
// week when nothing was measured.
func (r *Repository) Save(ctx context.Context, userID uuid.UUID, source string, readings []Reading) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, apperr.Wrap(err, "begin health ingest")
	}
	// Rollback after a successful Commit is a no-op, so this needs no flag.
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)
	for _, reading := range readings {
		if _, err := q.UpsertHealthMetric(ctx, healthdb.UpsertHealthMetricParams{
			UserID:    userID,
			Source:    source,
			Metric:    reading.Metric,
			Value:     reading.Value,
			Unit:      reading.Unit,
			StartedAt: reading.StartedAt,
			EndedAt:   reading.EndedAt,
		}); err != nil {
			return 0, apperr.Wrap(err, "upsert health reading %q", reading.Metric)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, apperr.Wrap(err, "commit health ingest")
	}
	return len(readings), nil
}

func (r *Repository) Between(ctx context.Context, userID uuid.UUID, metric string, since, until time.Time) ([]Stored, error) {
	rows, err := r.q.ListHealthMetricsBetween(ctx, healthdb.ListHealthMetricsBetweenParams{
		UserID:      userID,
		Metric:      metric,
		StartedAt:   since,
		StartedAt_2: until,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list health readings")
	}

	out := make([]Stored, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) DeleteBySource(ctx context.Context, userID uuid.UUID, source string) error {
	if err := r.q.DeleteHealthMetricsBySource(ctx, healthdb.DeleteHealthMetricsBySourceParams{
		UserID: userID,
		Source: source,
	}); err != nil {
		return apperr.Wrap(err, "delete health readings for %q", source)
	}
	return nil
}

func fromDB(row healthdb.HealthMetric) Stored {
	return Stored{
		ID:        row.ID,
		Source:    row.Source,
		Metric:    row.Metric,
		Value:     row.Value,
		Unit:      row.Unit,
		StartedAt: row.StartedAt,
		EndedAt:   row.EndedAt,
	}
}
