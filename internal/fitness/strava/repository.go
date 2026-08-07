package strava

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	stravadb "github.com/NorthAIProject/north-client/internal/fitness/strava/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *stravadb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: stravadb.New(pool)}
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, athleteID int64, access, refresh string, expiresAt time.Time, scopes string) (Connection, error) {
	row, err := r.q.UpsertStravaConnection(ctx, stravadb.UpsertStravaConnectionParams{
		UserID:       userID,
		AthleteID:    athleteID,
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    expiresAt,
		Scopes:       scopes,
	})
	if err != nil {
		// Deliberately not wrapping with the parameters: they include the
		// tokens.
		return Connection{}, apperr.Wrap(err, "save strava connection")
	}
	return fromDB(row), nil
}

func (r *Repository) UpdateTokens(ctx context.Context, userID uuid.UUID, access, refresh string, expiresAt time.Time) (Connection, error) {
	row, err := r.q.UpdateStravaTokens(ctx, stravadb.UpdateStravaTokensParams{
		UserID:       userID,
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, apperr.ErrNotFound
		}
		return Connection{}, apperr.Wrap(err, "update strava tokens")
	}
	return fromDB(row), nil
}

// Get returns apperr.ErrNotFound when the user has never connected, which is
// the normal state rather than a failure.
func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (Connection, error) {
	row, err := r.q.GetStravaConnection(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, apperr.ErrNotFound
		}
		return Connection{}, apperr.Wrap(err, "get strava connection")
	}
	return fromDB(row), nil
}

func (r *Repository) MarkSynced(ctx context.Context, userID uuid.UUID, at time.Time) error {
	err := r.q.MarkStravaSynced(ctx, stravadb.MarkStravaSyncedParams{
		UserID:       userID,
		LastSyncedAt: &at,
	})
	if err != nil {
		return apperr.Wrap(err, "mark strava synced")
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, userID uuid.UUID) error {
	if err := r.q.DeleteStravaConnection(ctx, userID); err != nil {
		return apperr.Wrap(err, "delete strava connection")
	}
	return nil
}

func fromDB(row stravadb.StravaConnection) Connection {
	return Connection{
		UserID:       row.UserID.String(),
		AthleteID:    row.AthleteID,
		AccessToken:  row.AccessToken,
		RefreshToken: row.RefreshToken,
		ExpiresAt:    row.ExpiresAt,
		Scopes:       row.Scopes,
		LastSyncedAt: row.LastSyncedAt,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}
