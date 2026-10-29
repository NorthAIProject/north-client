package lighterday

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	lighterdaydb "github.com/NorthAIProject/north-client/internal/lighterday/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// PostgresRepository keeps each day's lighter-or-keep answer in Postgres. It
// is the Repository the process wires into Service.
type PostgresRepository struct {
	q *lighterdaydb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{q: lighterdaydb.New(pool)}
}

func (r *PostgresRepository) SetChoice(ctx context.Context, userID uuid.UUID, day time.Time, choice, source string) error {
	if err := r.q.SetChoice(ctx, lighterdaydb.SetChoiceParams{UserID: userID, Day: date(day), Choice: choice, Source: source}); err != nil {
		return apperr.Wrap(err, "save lighter day")
	}
	return nil
}

// Choice is the answer given on day, or empty strings when there is none.
func (r *PostgresRepository) Choice(ctx context.Context, userID uuid.UUID, day time.Time) (choice, source string, err error) {
	row, err := r.q.GetChoice(ctx, lighterdaydb.GetChoiceParams{UserID: userID, Day: date(day)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", apperr.Wrap(err, "load lighter day")
	}
	return row.Choice, row.Source, nil
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
