package soreness

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	sorenessdb "github.com/NorthAIProject/north-client/internal/soreness/db"
)

type Repository struct {
	q *sorenessdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: sorenessdb.New(pool)}
}

func date(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, day time.Time, region string, severity int, note string) (Entry, error) {
	row, err := r.q.UpsertSoreness(ctx, sorenessdb.UpsertSorenessParams{
		UserID: userID, LogDate: date(day), Region: region, Severity: int16(severity), Note: note,
	})
	if err != nil {
		return Entry{}, apperr.Wrap(err, "upsert soreness")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, userID uuid.UUID, day time.Time, region string) error {
	if err := r.q.DeleteSoreness(ctx, sorenessdb.DeleteSorenessParams{UserID: userID, LogDate: date(day), Region: region}); err != nil {
		return apperr.Wrap(err, "delete soreness")
	}
	return nil
}

func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Entry, error) {
	rows, err := r.q.ListSorenessBetween(ctx, sorenessdb.ListSorenessBetweenParams{UserID: userID, LogDate: date(since), LogDate_2: date(until)})
	if err != nil {
		return nil, apperr.Wrap(err, "list soreness")
	}
	out := make([]Entry, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return out, nil
}

func fromDB(row sorenessdb.SorenessLog) Entry {
	return Entry{LogDate: row.LogDate.Time, Region: row.Region, Severity: int(row.Severity), Note: row.Note, LoggedAt: row.LoggedAt}
}
