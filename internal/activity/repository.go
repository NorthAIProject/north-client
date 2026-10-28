package activity

import (
	"context"
	"errors"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	activitydb "github.com/NorthAIProject/north-client/internal/activity/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type PostgresRepository struct {
	q *activitydb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{q: activitydb.New(pool)}
}

// Create opens a session. planWeekday is empty unless a guided workout was
// started from a plan day.
func (r *PostgresRepository) Create(ctx context.Context, userID uuid.UUID, activityCode, planWeekday string, weightKg float64) (Session, error) {
	weekday := util.StrZeroPtr(planWeekday)
	row, err := r.q.CreateActivitySession(ctx, activitydb.CreateActivitySessionParams{
		UserID:           userID,
		ActivityCode:     activityCode,
		WeightKgSnapshot: weightKg,
		PlanWeekday:      weekday,
	})
	if err != nil {
		return Session{}, apperr.Wrap(err, "create activity session")
	}
	return fromDB(row), nil
}

func (r *PostgresRepository) Get(ctx context.Context, id, userID uuid.UUID) (Session, error) {
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
func (r *PostgresRepository) Active(ctx context.Context, userID uuid.UUID) (Session, bool, error) {
	row, err := r.q.ActiveActivitySession(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, false, nil
		}
		return Session{}, false, apperr.Wrap(err, "active activity session")
	}
	return fromDB(row), true, nil
}

func (r *PostgresRepository) SetPaused(ctx context.Context, id, userID uuid.UUID) (Session, error) {
	row, err := r.q.PauseActivitySession(ctx, activitydb.PauseActivitySessionParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, apperr.ErrNotFound
		}
		return Session{}, apperr.Wrap(err, "pause activity session")
	}
	return fromDB(row), nil
}

func (r *PostgresRepository) SetResumed(ctx context.Context, id, userID uuid.UUID, totalPausedSeconds int) (Session, error) {
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

func (r *PostgresRepository) Complete(ctx context.Context, id, userID uuid.UUID, endedAt time.Time, calories float64) (Session, error) {
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

func (r *PostgresRepository) Cancel(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.CancelActivitySession(ctx, activitydb.CancelActivitySessionParams{ID: id, UserID: userID}), "cancel activity session")
}

func (r *PostgresRepository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Session, error) {
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

func (r *PostgresRepository) SumCaloriesSince(ctx context.Context, userID uuid.UUID, since time.Time) (float64, error) {
	total, err := r.q.SumActivityCaloriesSince(ctx, activitydb.SumActivityCaloriesSinceParams{UserID: userID, EndedAt: &since})
	if err != nil {
		return 0, apperr.Wrap(err, "sum activity calories")
	}
	return total, nil
}

// SumCaloriesBetween totals completed sessions in the half-open window
// [since, until). The bounded form is what lets a window be compared against
// the one before it without counting a boundary session twice.
func (r *PostgresRepository) SumCaloriesBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) (float64, error) {
	total, err := r.q.SumActivityCaloriesBetween(ctx, activitydb.SumActivityCaloriesBetweenParams{
		UserID:    userID,
		EndedAt:   &since,
		EndedAt_2: &until,
	})
	if err != nil {
		return 0, apperr.Wrap(err, "sum activity calories between")
	}
	return total, nil
}

// ListBetween returns the sessions finished in [since, until), newest first.
func (r *PostgresRepository) ListBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Session, error) {
	rows, err := r.q.ListActivitySessionsBetween(ctx, activitydb.ListActivitySessionsBetweenParams{
		UserID:    userID,
		EndedAt:   &since,
		EndedAt_2: &until,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list activity sessions between")
	}

	out := make([]Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

// CompletedByIDs is the completed sessions among ids, whoever owns them.
// Callers authorize: nothing here checks that the reader may see them.
func (r *PostgresRepository) CompletedByIDs(ctx context.Context, ids []uuid.UUID) ([]Session, error) {
	rows, err := r.q.CompletedSessionsByIDs(ctx, ids)
	if err != nil {
		return nil, apperr.Wrap(err, "completed activity sessions by ids")
	}
	return fromRows(rows), nil
}

// ListCompleted is one person's completed sessions that ended before a
// point, newest first.
func (r *PostgresRepository) ListCompleted(ctx context.Context, userID uuid.UUID, before time.Time, limit int) ([]Session, error) {
	rows, err := r.q.ListCompletedSessions(ctx, activitydb.ListCompletedSessionsParams{UserID: userID, Before: &before, Lim: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list completed activity sessions")
	}
	return fromRows(rows), nil
}

// CountCompleted is how many sessions the person has finished.
func (r *PostgresRepository) CountCompleted(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountCompletedSessions(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count completed activity sessions")
	}
	return int(n), nil
}

func fromRows(rows []activitydb.ActivitySession) []Session {
	out := make([]Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
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
		DistanceM:          row.DistanceM,
		AvgHR:              widen(row.AvgHr),
		MaxHR:              widen(row.MaxHr),
		ElevationM:         row.ElevationM,
		Indoor:             row.Indoor,
		ExternalID:         row.ExternalID,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		PlanWeekday:        util.Val(row.PlanWeekday),
	}
}

// widen reads a real column as the float64 the domain uses.
func widen(v *float32) *float64 {
	if v == nil {
		return nil
	}
	return util.Ptr(float64(*v))
}

// narrow writes a measured figure to a real column; zero is "not measured".
func narrow(v float64) *float32 {
	if v <= 0 {
		return nil
	}
	return util.Ptr(float32(v))
}

// positive is a measured figure, or nil for "not measured".
func positive(v float64) *float64 {
	if v <= 0 {
		return nil
	}
	return util.Ptr(v)
}

// Import writes an already-finished session from a provider sync.
//
// Distinct from Create because the in-app lifecycle (start, pause, stop)
// does not apply: a synced session arrives complete. Returns false when the
// row already existed — the ON CONFLICT DO NOTHING on
// UNIQUE (source, external_id) makes re-importing the same activity a no-op
// rather than an error, which is what lets a sync run as often as it likes.
func (r *PostgresRepository) Import(ctx context.Context, in ImportInput) (Session, bool, error) {
	endedAt := in.EndedAt
	externalID := in.ExternalID

	// Unknown calories are stored as NULL, not as zero.
	//
	// They are genuinely unknown when the provider sent no figure and we hold
	// no body weight to estimate from — Strava's list endpoint omits calories,
	// and a new account has no biometrics yet. Writing 0 would be a claim that
	// an hour of training burned nothing, and it would flow straight into the
	// coach's "kcal burned today" line as fact.
	var calories *float64
	if in.Calories > 0 {
		calories = util.Ptr(in.Calories)
	}

	row, err := r.q.ImportActivitySession(ctx, activitydb.ImportActivitySessionParams{
		UserID:           in.UserID,
		ActivityCode:     in.ActivityCode,
		Source:           in.Source,
		WeightKgSnapshot: in.WeightKg,
		StartedAt:        in.StartedAt,
		EndedAt:          &endedAt,
		CaloriesBurned:   calories,
		ExternalID:       &externalID,
		DistanceM:        positive(in.DistanceM),
		AvgHr:            narrow(in.AvgHR),
		MaxHr:            narrow(in.MaxHR),
		ElevationM:       positive(in.ElevationM),
		Indoor:           in.Indoor,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already imported. Fill in what this import knows that the
			// first did not, so a re-sync backfills new measurements.
			if fillErr := r.q.FillImportedActivitySession(ctx, activitydb.FillImportedActivitySessionParams{
				DistanceM:  positive(in.DistanceM),
				AvgHr:      narrow(in.AvgHR),
				MaxHr:      narrow(in.MaxHR),
				ElevationM: positive(in.ElevationM),
				Indoor:     in.Indoor,
				UserID:     in.UserID,
				Source:     in.Source,
				ExternalID: &externalID,
			}); fillErr != nil {
				return Session{}, false, apperr.Wrap(fillErr, "fill imported activity session")
			}
			return Session{}, false, nil
		}
		return Session{}, false, apperr.Wrap(err, "import activity session")
	}
	return fromDB(row), true, nil
}

// Log writes a finished session the person entered themselves.
//
// Distinct from Import because there is no external id to dedupe on and the
// source is always manual; distinct from Create because the session arrives
// complete. Distance is stored as NULL rather than zero when it was not
// given: a yoga session did not go nowhere, it just was not measured.
func (r *PostgresRepository) Log(ctx context.Context, userID uuid.UUID, in LogInput, weightKg, calories float64) (Session, error) {
	endedAt := in.StartedAt.Add(in.Duration)

	var distance *float64
	if in.DistanceM > 0 {
		distance = util.Ptr(in.DistanceM)
	}

	row, err := r.q.LogActivitySession(ctx, activitydb.LogActivitySessionParams{
		UserID:           userID,
		ActivityCode:     in.ActivityCode,
		WeightKgSnapshot: weightKg,
		StartedAt:        in.StartedAt,
		EndedAt:          &endedAt,
		CaloriesBurned:   &calories,
		DistanceM:        distance,
	})
	if err != nil {
		return Session{}, apperr.Wrap(err, "log activity session")
	}
	return fromDB(row), nil
}
