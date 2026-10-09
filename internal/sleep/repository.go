package sleep

import (
	"context"
	"errors"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	sleepdb "github.com/NorthAIProject/north-client/internal/sleep/db"
)

type Repository struct {
	q *sleepdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: sleepdb.New(pool)}
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, date time.Time, in Log) (Log, error) {
	var quality *int16
	if in.Quality != nil {
		quality = util.Ptr(int16(*in.Quality))
	}

	row, err := r.q.UpsertSleepLog(ctx, sleepdb.UpsertSleepLogParams{
		UserID:          userID,
		LocalDate:       toDate(date),
		DurationMinutes: int32(in.DurationMinutes),
		Quality:         quality,
		// The columns are nullable and CHECK-constrained to "HH:MM", so an empty
		// string has to become NULL rather than fail the constraint.
		Bedtime:  util.StrZeroPtr(in.Bedtime),
		WakeTime: util.StrZeroPtr(in.WakeTime),
		Notes:    in.Notes,
	})
	if err != nil {
		return Log{}, apperr.Wrap(err, "upsert sleep log")
	}
	return fromDB(row), nil
}

// ForDate returns apperr.ErrNotFound when nothing was logged that night,
// which is an ordinary state rather than a failure.
func (r *Repository) ForDate(ctx context.Context, userID uuid.UUID, date time.Time) (Log, error) {
	row, err := r.q.GetSleepLogForDate(ctx, sleepdb.GetSleepLogForDateParams{
		UserID:    userID,
		LocalDate: toDate(date),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Log{}, apperr.ErrNotFound
		}
		return Log{}, apperr.Wrap(err, "get sleep log")
	}
	return fromDB(row), nil
}

func (r *Repository) Recent(ctx context.Context, userID uuid.UUID, limit int) ([]Log, error) {
	rows, err := r.q.ListSleepLogs(ctx, sleepdb.ListSleepLogsParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list sleep logs")
	}

	out := make([]Log, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

// ListBetween returns the nights logged in the half-open window
// [since, until), newest first.
func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Log, error) {
	rows, err := r.q.ListSleepLogsBetween(ctx, sleepdb.ListSleepLogsBetweenParams{
		UserID:      userID,
		LocalDate:   toDate(since),
		LocalDate_2: toDate(until),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list sleep logs between")
	}

	out := make([]Log, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	if err := r.q.DeleteSleepLog(ctx, sleepdb.DeleteSleepLogParams{ID: id, UserID: userID}); err != nil {
		return apperr.Wrap(err, "delete sleep log")
	}
	return nil
}

func fromDB(row sleepdb.SleepLog) Log {
	l := Log{
		ID:              row.ID,
		UserID:          row.UserID,
		LocalDate:       fromDate(row.LocalDate),
		DurationMinutes: int(row.DurationMinutes),
		Notes:           row.Notes,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
	if row.Quality != nil {
		l.Quality = util.Ptr(int(*row.Quality))
	}
	l.Bedtime = util.Val(row.Bedtime)
	l.WakeTime = util.Val(row.WakeTime)
	return l
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
