package caffeine

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	caffeinedb "github.com/NorthAIProject/north-client/internal/caffeine/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *caffeinedb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: caffeinedb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, date time.Time, mg int, label string, at time.Time) (Entry, error) {
	row, err := r.q.CreateCaffeineEntry(ctx, caffeinedb.CreateCaffeineEntryParams{
		UserID: userID, LogDate: pgtype.Date{Time: date, Valid: true}, Mg: int32(mg), Label: label, LoggedAt: at,
	})
	if err != nil {
		return Entry{}, apperr.Wrap(err, "create caffeine entry")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteCaffeineEntry(ctx, caffeinedb.DeleteCaffeineEntryParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete caffeine entry")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Entry, error) {
	rows, err := r.q.ListCaffeineBetween(ctx, caffeinedb.ListCaffeineBetweenParams{UserID: userID, LoggedAt: since, LoggedAt_2: until})
	if err != nil {
		return nil, apperr.Wrap(err, "list caffeine")
	}
	out := make([]Entry, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return out, nil
}

func fromDB(row caffeinedb.CaffeineLog) Entry {
	return Entry{ID: row.ID, LogDate: row.LogDate.Time, MG: int(row.Mg), Label: row.Label, LoggedAt: row.LoggedAt}
}
