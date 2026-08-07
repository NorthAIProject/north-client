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

// SaveActivity records Strava's own view of an activity, alongside the
// normalised session the import produced. Upserted rather than inserted, so a
// renamed or corrected activity updates instead of duplicating.
func (r *Repository) SaveActivity(ctx context.Context, userID uuid.UUID, a Activity) error {
	err := r.q.UpsertStravaActivity(ctx, stravadb.UpsertStravaActivityParams{
		UserID:              userID,
		StravaID:            a.StravaID,
		Name:                a.Name,
		SportType:           a.SportType,
		StartDate:           a.StartDate,
		DistanceM:           a.DistanceM,
		MovingTimeS:         int32(a.MovingTimeS),
		ElapsedTimeS:        int32(a.ElapsedTimeS),
		TotalElevationGainM: a.ElevationGainM,
		AverageSpeedMs:      a.AverageSpeedMS,
		SummaryPolyline:     a.SummaryPolyline,
	})
	if err != nil {
		return apperr.Wrap(err, "save strava activity")
	}
	return nil
}

// RecentActivities returns the newest activities first, for the 3D view.
func (r *Repository) RecentActivities(ctx context.Context, userID uuid.UUID, limit int) ([]Activity, error) {
	rows, err := r.q.ListStravaActivities(ctx, stravadb.ListStravaActivitiesParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list strava activities")
	}

	out := make([]Activity, 0, len(rows))
	for _, row := range rows {
		out = append(out, Activity{
			StravaID:        row.StravaID,
			Name:            row.Name,
			SportType:       row.SportType,
			StartDate:       row.StartDate,
			DistanceM:       row.DistanceM,
			MovingTimeS:     int(row.MovingTimeS),
			ElapsedTimeS:    int(row.ElapsedTimeS),
			ElevationGainM:  row.TotalElevationGainM,
			AverageSpeedMS:  row.AverageSpeedMs,
			SummaryPolyline: row.SummaryPolyline,
		})
	}
	return out, nil
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
