package lifts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	liftsdb "github.com/NorthAIProject/north-client/internal/lifts/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *liftsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: liftsdb.New(pool)}
}

// Create logs a set. A clientID this account already logged a set under
// returns that set instead of logging another; nil always logs a new one.
func (r *Repository) Create(ctx context.Context, userID uuid.UUID, s Set, clientID *uuid.UUID) (Set, error) {
	row, err := r.q.CreateSetLog(ctx, liftsdb.CreateSetLogParams{
		UserID:            userID,
		ActivitySessionID: s.ActivitySessionID,
		LogDate:           pgtype.Date{Time: s.LogDate, Valid: true},
		ExerciseSlug:      s.ExerciseSlug,
		ExerciseName:      s.ExerciseName,
		SetNumber:         int32(s.SetNumber),
		WeightKg:          s.WeightKg,
		Reps:              int32(s.Reps),
		PerformedAt:       s.PerformedAt,
		Kind:              s.Kind,
		Rir:               rirToDB(s.RIR),
		ClientID:          clientID,
	})
	if err != nil {
		return Set{}, apperr.Wrap(err, "create set log")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteSetLog(ctx, liftsdb.DeleteSetLogParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete set log")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *Repository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Set, error) {
	rows, err := r.q.ListSetsBetween(ctx, liftsdb.ListSetsBetweenParams{UserID: userID, PerformedAt: since, PerformedAt_2: until})
	if err != nil {
		return nil, apperr.Wrap(err, "list sets")
	}
	return fromRows(rows), nil
}

func (r *Repository) ListForExercises(ctx context.Context, userID uuid.UUID, keys []string, since time.Time) ([]Set, error) {
	rows, err := r.q.ListSetsForExercises(ctx, liftsdb.ListSetsForExercisesParams{UserID: userID, Since: since, Keys: keys})
	if err != nil {
		return nil, apperr.Wrap(err, "list sets for exercises")
	}
	return fromRows(rows), nil
}

// ListForSession is one timed workout's sets, in the order they were done.
func (r *Repository) ListForSession(ctx context.Context, userID, sessionID uuid.UUID) ([]Set, error) {
	rows, err := r.q.ListSetsForSession(ctx, liftsdb.ListSetsForSessionParams{UserID: userID, ActivitySessionID: &sessionID})
	if err != nil {
		return nil, apperr.Wrap(err, "list sets for session")
	}
	return fromRows(rows), nil
}

// ListForExercisesBefore is the history of these exercises in [since, before),
// newest first.
func (r *Repository) ListForExercisesBefore(ctx context.Context, userID uuid.UUID, keys []string, since, before time.Time) ([]Set, error) {
	rows, err := r.q.ListSetsForExercisesBefore(ctx, liftsdb.ListSetsForExercisesBeforeParams{UserID: userID, Since: since, Before: before, Keys: keys})
	if err != nil {
		return nil, apperr.Wrap(err, "list earlier sets for exercises")
	}
	return fromRows(rows), nil
}

func fromRows(rows []liftsdb.SetLog) []Set {
	out := make([]Set, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return out
}

func fromDB(row liftsdb.SetLog) Set {
	return Set{
		ID:                row.ID,
		ActivitySessionID: row.ActivitySessionID,
		LogDate:           row.LogDate.Time,
		ExerciseSlug:      row.ExerciseSlug,
		ExerciseName:      row.ExerciseName,
		SetNumber:         int(row.SetNumber),
		WeightKg:          row.WeightKg,
		Reps:              int(row.Reps),
		PerformedAt:       row.PerformedAt,
		Kind:              row.Kind,
		RIR:               rirFromDB(row.Rir),
	}
}

func rirToDB(rir *int) *int16 {
	if rir == nil {
		return nil
	}
	v := int16(*rir)
	return &v
}

func rirFromDB(rir *int16) *int {
	if rir == nil {
		return nil
	}
	v := int(*rir)
	return &v
}
