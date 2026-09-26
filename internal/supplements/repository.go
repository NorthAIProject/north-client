package supplements

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	supplementsdb "github.com/NorthAIProject/north-client/internal/supplements/db"
)

type Repository struct {
	q *supplementsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: supplementsdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, date time.Time, e Entry) (Entry, error) {
	nutrients := e.Nutrients
	if nutrients == nil {
		nutrients = []string{}
	}
	row, err := r.q.CreateSupplementEntry(ctx, supplementsdb.CreateSupplementEntryParams{
		UserID: userID, LogDate: pgtype.Date{Time: date, Valid: true}, Name: e.Name,
		Count: int16(e.Count), Nutrients: nutrients, LoggedAt: e.LoggedAt,
	})
	if err != nil {
		return Entry{}, apperr.Wrap(err, "create supplement entry")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteSupplementEntry(ctx, supplementsdb.DeleteSupplementEntryParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete supplement entry")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Entry, error) {
	rows, err := r.q.ListSupplementsBetween(ctx, supplementsdb.ListSupplementsBetweenParams{UserID: userID, LoggedAt: since, LoggedAt_2: until})
	if err != nil {
		return nil, apperr.Wrap(err, "list supplements")
	}
	out := make([]Entry, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return out, nil
}

func fromDB(row supplementsdb.SupplementLog) Entry {
	return Entry{
		ID: row.ID, LogDate: row.LogDate.Time, Name: row.Name, Count: int(row.Count),
		Nutrients: row.Nutrients, LoggedAt: row.LoggedAt,
	}
}
