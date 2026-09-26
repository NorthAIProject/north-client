package fasting

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	fastingdb "github.com/NorthAIProject/north-client/internal/fasting/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *fastingdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: fastingdb.New(pool)}
}

// Start opens a fast. A second open fast violates the one-open index and is
// reported as a conflict.
func (r *Repository) Start(ctx context.Context, userID uuid.UUID, at time.Time, targetHours int) (Session, error) {
	row, err := r.q.StartFast(ctx, fastingdb.StartFastParams{UserID: userID, StartedAt: at, TargetHours: int16(targetHours)})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Session{}, apperr.Wrap(apperr.ErrConflict, "a fast is already running")
		}
		return Session{}, apperr.Wrap(err, "start fast")
	}
	return fromDB(row), nil
}

func (r *Repository) Stop(ctx context.Context, userID uuid.UUID, at time.Time) (Session, error) {
	row, err := r.q.StopFast(ctx, fastingdb.StopFastParams{UserID: userID, EndedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, apperr.ErrNotFound
	}
	if err != nil {
		return Session{}, apperr.Wrap(err, "stop fast")
	}
	return fromDB(row), nil
}

func (r *Repository) Open(ctx context.Context, userID uuid.UUID) (Session, bool, error) {
	row, err := r.q.OpenFast(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, apperr.Wrap(err, "open fast")
	}
	return fromDB(row), true, nil
}

func (r *Repository) Overlapping(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Session, error) {
	rows, err := r.q.ListFastsOverlapping(ctx, fastingdb.ListFastsOverlappingParams{UserID: userID, EndedAt: &since, StartedAt: until})
	if err != nil {
		return nil, apperr.Wrap(err, "list fasts")
	}
	out := make([]Session, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return out, nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteFast(ctx, fastingdb.DeleteFastParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete fast")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func fromDB(row fastingdb.FastingSession) Session {
	return Session{ID: row.ID, StartedAt: row.StartedAt, EndedAt: row.EndedAt, TargetHours: int(row.TargetHours)}
}
