package milestones

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	milestonesdb "github.com/NorthAIProject/north-client/internal/milestones/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *milestonesdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: milestonesdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, name string, last time.Time, interval *int) (Tracker, error) {
	var iv *int16
	if interval != nil {
		v := int16(*interval)
		iv = &v
	}
	row, err := r.q.CreateMilestone(ctx, milestonesdb.CreateMilestoneParams{
		UserID: userID, Name: name, LastDoneOn: pgtype.Date{Time: last, Valid: true}, IntervalMonths: iv,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Tracker{}, apperr.FieldErrors{}.Add("name", "You already track that.")
		}
		return Tracker{}, apperr.Wrap(err, "create milestone")
	}
	return fromDB(row), nil
}

func (r *Repository) MarkDone(ctx context.Context, id, userID uuid.UUID, on time.Time) (Tracker, error) {
	row, err := r.q.MarkMilestoneDone(ctx, milestonesdb.MarkMilestoneDoneParams{ID: id, UserID: userID, LastDoneOn: pgtype.Date{Time: on, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return Tracker{}, apperr.ErrNotFound
	}
	if err != nil {
		return Tracker{}, apperr.Wrap(err, "mark milestone done")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteMilestone(ctx, milestonesdb.DeleteMilestoneParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete milestone")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID) ([]Tracker, error) {
	rows, err := r.q.ListMilestones(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list milestones")
	}
	out := make([]Tracker, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return out, nil
}

func fromDB(row milestonesdb.MilestoneTracker) Tracker {
	t := Tracker{ID: row.ID, Name: row.Name, LastDoneOn: row.LastDoneOn.Time}
	if row.IntervalMonths != nil {
		v := int(*row.IntervalMonths)
		t.IntervalMonths = &v
	}
	return t
}
