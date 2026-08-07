package activity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	activitydb "github.com/NorthAIProject/north-client/internal/activity/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *activitydb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: activitydb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, activityCode string, weightKg float64) (Session, error) {
	row, err := r.q.CreateActivitySession(ctx, activitydb.CreateActivitySessionParams{
		UserID:           userID,
		ActivityCode:     activityCode,
		WeightKgSnapshot: weightKg,
	})
	if err != nil {
		return Session{}, apperr.Wrap(err, "create activity session")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Session, error) {
	row, err := r.q.GetActivitySession(ctx, activitydb.GetActivitySessionParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, apperr.ErrNotFound
		}
		return Session{}, apperr.Wrap(err, "get activity session")
	}
	return fromDB(row), nil
}

// Active returns the user's open session, if any. Unlike Get, "none open" is
// a normal state, not an error, so it reports via the bool rather than
// apperr.ErrNotFound.
func (r *Repository) Active(ctx context.Context, userID uuid.UUID) (Session, bool, error) {
	row, err := r.q.ActiveActivitySession(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, false, nil
		}
		return Session{}, false, apperr.Wrap(err, "active activity session")
	}
	return fromDB(row), true, nil
}

func (r *Repository) SetPaused(ctx context.Context, id, userID uuid.UUID) (Session, error) {
	row, err := r.q.PauseActivitySession(ctx, activitydb.PauseActivitySessionParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, apperr.ErrNotFound
		}
		return Session{}, apperr.Wrap(err, "pause activity session")
	}
	return fromDB(row), nil
}

func (r *Repository) SetResumed(ctx context.Context, id, userID uuid.UUID, totalPausedSeconds int) (Session, error) {
	row, err := r.q.ResumeActivitySession(ctx, activitydb.ResumeActivitySessionParams{
		ID:                 id,
		UserID:             userID,
		TotalPausedSeconds: int32(totalPausedSeconds),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, apperr.ErrNotFound
		}
		return Session{}, apperr.Wrap(err, "resume activity session")
	}
	return fromDB(row), nil
}

func (r *Repository) Complete(ctx context.Context, id, userID uuid.UUID, endedAt time.Time, calories float64) (Session, error) {
	row, err := r.q.CompleteActivitySession(ctx, activitydb.CompleteActivitySessionParams{
		ID:             id,
		UserID:         userID,
		EndedAt:        &endedAt,
		CaloriesBurned: &calories,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, apperr.ErrNotFound
		}
		return Session{}, apperr.Wrap(err, "complete activity session")
	}
	return fromDB(row), nil
}

func (r *Repository) Cancel(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.CancelActivitySession(ctx, activitydb.CancelActivitySessionParams{ID: id, UserID: userID}), "cancel activity session")
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Session, error) {
	rows, err := r.q.ListActivitySessions(ctx, activitydb.ListActivitySessionsParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list activity sessions")
	}

	out := make([]Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) SumCaloriesSince(ctx context.Context, userID uuid.UUID, since time.Time) (float64, error) {
	total, err := r.q.SumActivityCaloriesSince(ctx, activitydb.SumActivityCaloriesSinceParams{UserID: userID, EndedAt: &since})
	if err != nil {
		return 0, apperr.Wrap(err, "sum activity calories")
	}
	return total, nil
}

func fromDB(row activitydb.ActivitySession) Session {
	return Session{
		ID:                 row.ID,
		UserID:             row.UserID,
		ActivityCode:       row.ActivityCode,
		Source:             row.Source,
		Status:             row.Status,
		WeightKgSnapshot:   row.WeightKgSnapshot,
		StartedAt:          row.StartedAt,
		PausedAt:           row.PausedAt,
		TotalPausedSeconds: int(row.TotalPausedSeconds),
		EndedAt:            row.EndedAt,
		CaloriesBurned:     row.CaloriesBurned,
		ExternalID:         row.ExternalID,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

// Import writes an already-finished session from a provider sync.
//
// Distinct from Create because the in-app lifecycle (start, pause, stop)
// does not apply: a synced session arrives complete. Returns false when the
// row already existed — the ON CONFLICT DO NOTHING on
// UNIQUE (source, external_id) makes re-importing the same activity a no-op
// rather than an error, which is what lets a sync run as often as it likes.
func (r *Repository) Import(ctx context.Context, in ImportInput) (Session, bool, error) {
	endedAt := in.EndedAt
	calories := in.Calories
	externalID := in.ExternalID

	row, err := r.q.ImportActivitySession(ctx, activitydb.ImportActivitySessionParams{
		UserID:           in.UserID,
		ActivityCode:     in.ActivityCode,
		Source:           in.Source,
		WeightKgSnapshot: in.WeightKg,
		StartedAt:        in.StartedAt,
		EndedAt:          &endedAt,
		CaloriesBurned:   &calories,
		ExternalID:       &externalID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, false, nil // already imported
		}
		return Session{}, false, apperr.Wrap(err, "import activity session")
	}
	return fromDB(row), true, nil
}
