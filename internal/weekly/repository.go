package weekly

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
	weeklydb "github.com/NorthAIProject/north-client/internal/weekly/db"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// PostgresRepository keeps each week's focus in Postgres. It is the
// Repository the process wires into Service.
type PostgresRepository struct {
	q *weeklydb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{q: weeklydb.New(pool)}
}

// UpsertFocus stores the focus for the week starting at weekStart, read back
// with its week start in loc.
func (r *PostgresRepository) UpsertFocus(ctx context.Context, userID uuid.UUID, weekStart time.Time, in Input, loc *time.Location) (Focus, error) {
	row, err := r.q.UpsertFocus(ctx, weeklydb.UpsertFocusParams{
		UserID: userID, WeekStart: date(weekStart), Priorities: in.Priorities, Volume: string(in.Volume),
	})
	if err != nil {
		return Focus{}, apperr.Wrap(err, "save weekly focus")
	}
	return fromDB(row, loc), nil
}

// Focus is the focus for the week starting at monday, or nil when the week
// was never reviewed.
func (r *PostgresRepository) Focus(ctx context.Context, userID uuid.UUID, monday time.Time) (*Focus, error) {
	row, err := r.q.GetFocus(ctx, weeklydb.GetFocusParams{UserID: userID, WeekStart: date(monday)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Wrap(err, "load weekly focus")
	}
	return util.Ptr(fromDB(row, monday.Location())), nil
}

func fromDB(row weeklydb.WeeklyFocus, loc *time.Location) Focus {
	d := row.WeekStart.Time
	return Focus{
		WeekStart:  time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc),
		Priorities: row.Priorities,
		Volume:     plan.Volume(row.Volume),
		ReviewedAt: row.ReviewedAt,
	}
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
