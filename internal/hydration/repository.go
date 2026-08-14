package hydration

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	hydrationdb "github.com/NorthAIProject/north-client/internal/hydration/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *hydrationdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: hydrationdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, date time.Time, amountML int) (Entry, error) {
	row, err := r.q.CreateHydrationEntry(ctx, hydrationdb.CreateHydrationEntryParams{
		UserID:   userID,
		LogDate:  toDate(date),
		AmountMl: int32(amountML),
	})
	if err != nil {
		return Entry{}, apperr.Wrap(err, "create hydration entry")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	if err := r.q.DeleteHydrationEntry(ctx, hydrationdb.DeleteHydrationEntryParams{ID: id, UserID: userID}); err != nil {
		return apperr.Wrap(err, "delete hydration entry")
	}
	return nil
}

func (r *Repository) ListForDate(ctx context.Context, userID uuid.UUID, date time.Time) ([]Entry, error) {
	rows, err := r.q.ListHydrationEntriesForDate(ctx, hydrationdb.ListHydrationEntriesForDateParams{
		UserID:  userID,
		LogDate: toDate(date),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list hydration entries")
	}

	out := make([]Entry, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

// TotalForDate returns the day's millilitres and how many entries made it up.
func (r *Repository) TotalForDate(ctx context.Context, userID uuid.UUID, date time.Time) (int, int, error) {
	row, err := r.q.SumHydrationForDate(ctx, hydrationdb.SumHydrationForDateParams{
		UserID:  userID,
		LogDate: toDate(date),
	})
	if err != nil {
		return 0, 0, apperr.Wrap(err, "sum hydration for date")
	}
	return int(row.TotalMl), int(row.EntryCount), nil
}

// TotalsSince returns one row per day that has entries, most recent first.
// Days with nothing logged are absent rather than zero — the caller knows
// which dates it asked about and can fill the gaps if it needs them.
func (r *Repository) TotalsSince(ctx context.Context, userID uuid.UUID, since time.Time) ([]Day, error) {
	rows, err := r.q.SumHydrationByDateSince(ctx, hydrationdb.SumHydrationByDateSinceParams{
		UserID:  userID,
		LogDate: toDate(since),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "sum hydration by date")
	}

	out := make([]Day, 0, len(rows))
	for _, row := range rows {
		out = append(out, Day{
			Date:     fromDate(row.LogDate),
			TotalML:  int(row.TotalMl),
			Entries:  int(row.EntryCount),
			TargetML: DefaultDailyTargetML,
		})
	}
	return out, nil
}

// TotalsBetween returns one row per day that has entries in the half-open
// window [since, until). Days with nothing logged are absent rather than zero,
// same as TotalsSince.
func (r *Repository) TotalsBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Day, error) {
	rows, err := r.q.SumHydrationByDateBetween(ctx, hydrationdb.SumHydrationByDateBetweenParams{
		UserID:    userID,
		LogDate:   toDate(since),
		LogDate_2: toDate(until),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "sum hydration by date between")
	}

	out := make([]Day, 0, len(rows))
	for _, row := range rows {
		out = append(out, Day{
			Date:     fromDate(row.LogDate),
			TotalML:  int(row.TotalMl),
			Entries:  int(row.EntryCount),
			TargetML: DefaultDailyTargetML,
		})
	}
	return out, nil
}

// ListBetween returns the individual entries in [since, until), newest first.
// The activity timeline needs each pour, not the daily total.
func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Entry, error) {
	rows, err := r.q.ListHydrationEntriesBetween(ctx, hydrationdb.ListHydrationEntriesBetweenParams{
		UserID:    userID,
		LogDate:   toDate(since),
		LogDate_2: toDate(until),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list hydration entries between")
	}

	out := make([]Entry, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func fromDB(row hydrationdb.HydrationLog) Entry {
	return Entry{
		ID:       row.ID,
		UserID:   row.UserID,
		LogDate:  fromDate(row.LogDate),
		AmountML: int(row.AmountMl),
		LoggedAt: row.LoggedAt,
	}
}

// Date conversion is duplicated per slice rather than shared, same as
// localMidnight, so feature slices stay independent.
func toDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func fromDate(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return d.Time
}
