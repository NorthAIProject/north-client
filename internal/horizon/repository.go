package horizon

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	horizondb "github.com/NorthAIProject/north-client/internal/horizon/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Repository keeps forecasts as they were made.
type Repository struct {
	q *horizondb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: horizondb.New(pool)}
}

// Snapshot is one forecast as it was made: where a series was expected to
// be HorizonWeeks after MadeOn.
type Snapshot struct {
	// MadeOn is the person's calendar date, as midnight UTC.
	MadeOn       time.Time
	SeriesKey    string
	HorizonWeeks int
	Low          float64
	Mid          float64
	High         float64
}

// Add records snapshots. One already recorded for its day is left as it is.
func (r *Repository) Add(ctx context.Context, userID uuid.UUID, snapshots []Snapshot) error {
	for _, s := range snapshots {
		err := r.q.AddSnapshot(ctx, horizondb.AddSnapshotParams{
			UserID: userID, MadeOn: date(s.MadeOn), SeriesKey: s.SeriesKey, HorizonWeeks: int32(s.HorizonWeeks), //nolint:gosec // a small constant
			Low: s.Low, Mid: s.Mid, High: s.High,
		})
		if err != nil {
			return apperr.Wrap(err, "record forecast %s", s.SeriesKey)
		}
	}
	return nil
}

// List is the snapshots at one horizon made on or after since, by series
// then by day.
func (r *Repository) List(ctx context.Context, userID uuid.UUID, horizonWeeks int, since time.Time) ([]Snapshot, error) {
	rows, err := r.q.ListSnapshots(ctx, horizondb.ListSnapshotsParams{UserID: userID, HorizonWeeks: int32(horizonWeeks), MadeOn: date(since)}) //nolint:gosec // a small constant
	if err != nil {
		return nil, apperr.Wrap(err, "list forecasts")
	}
	out := make([]Snapshot, len(rows))
	for i, row := range rows {
		out[i] = Snapshot{MadeOn: row.MadeOn.Time, SeriesKey: row.SeriesKey, HorizonWeeks: int(row.HorizonWeeks), Low: row.Low, Mid: row.Mid, High: row.High}
	}
	return out, nil
}

// LatestDay is the last day a forecast was recorded for the person; the
// zero time when none was.
func (r *Repository) LatestDay(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	day, err := r.q.LatestSnapshotDay(ctx, userID)
	if err != nil {
		return time.Time{}, apperr.Wrap(err, "latest forecast day")
	}
	if !day.Valid || day.Time.Year() <= 1 {
		return time.Time{}, nil
	}
	return day.Time, nil
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
