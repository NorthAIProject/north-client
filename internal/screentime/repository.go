package screentime

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	screentimedb "github.com/NorthAIProject/north-client/internal/screentime/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *screentimedb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: screentimedb.New(pool)}
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, date time.Time, minutes int, source string) (Day, error) {
	row, err := r.q.UpsertScreenTime(ctx, screentimedb.UpsertScreenTimeParams{
		UserID: userID, LocalDate: pgtype.Date{Time: date, Valid: true}, Minutes: int32(minutes), Source: source,
	})
	if err != nil {
		return Day{}, apperr.Wrap(err, "upsert screen time")
	}
	return fromDB(row), nil
}

func (r *Repository) ForDate(ctx context.Context, userID uuid.UUID, date time.Time) (Day, bool, error) {
	row, err := r.q.ScreenTimeForDate(ctx, screentimedb.ScreenTimeForDateParams{UserID: userID, LocalDate: pgtype.Date{Time: date, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return Day{}, false, nil
	}
	if err != nil {
		return Day{}, false, apperr.Wrap(err, "screen time for date")
	}
	return fromDB(row), true, nil
}

func fromDB(row screentimedb.ScreenTimeLog) Day {
	return Day{LocalDate: row.LocalDate.Time, Minutes: int(row.Minutes), Source: row.Source}
}
