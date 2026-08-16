package strava

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/activity"
	stravadb "github.com/NorthAIProject/north-client/internal/fitness/strava/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/secret"
)

type Repository struct {
	q *stravadb.Queries

	// sealer encrypts the tokens at rest. Nil is a supported state, not a
	// misconfiguration: a deployment with no ENCRYPTION_KEY keeps the plaintext
	// behaviour 00022 shipped with, because losing the Strava integration
	// entirely would be a worse answer than the debt it already carries.
	sealer *secret.Sealer
}

func NewRepository(pool *pgxpool.Pool, sealer *secret.Sealer) *Repository {
	return &Repository{q: stravadb.New(pool), sealer: sealer}
}

// seal turns a token into the pair of column values that store it: sealed when
// there is a key, plaintext when there is not. Exactly one is ever populated,
// so a row cannot keep a readable copy beside an encrypted one.
//
// The user id is the additional data, so a row copied to another user fails to
// open rather than handing one person another's Strava account.
func (r *Repository) seal(userID uuid.UUID, token string) (plain string, sealed []byte, err error) {
	if r.sealer == nil {
		return token, nil, nil
	}
	sealed, err = r.sealer.Seal(userID[:], []byte(token))
	if err != nil {
		// Never wrapped with the token.
		return "", nil, errors.New("seal strava token")
	}
	return "", sealed, nil
}

// open reverses seal, preferring the sealed column.
//
// The plaintext fallback is what lets rows written before the key existed keep
// working. They rewrite themselves sealed at the next token refresh, which
// Strava forces within six hours — so the plaintext drains without a backfill.
func (r *Repository) open(userID uuid.UUID, plain string, sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return plain, nil
	}
	if r.sealer == nil {
		// The row was written by a process that had a key and this one does
		// not. Reconnecting would silently overwrite an encrypted credential
		// with a plaintext one, so refuse instead.
		return "", errors.New("strava token is encrypted but this process has no encryption key")
	}
	token, err := r.sealer.Open(userID[:], sealed)
	if err != nil {
		return "", apperr.Wrap(err, "open strava token")
	}
	return string(token), nil
}

func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, athleteID int64, access, refresh string, expiresAt time.Time, scopes string) (Connection, error) {
	accessPlain, accessSealed, err := r.seal(userID, access)
	if err != nil {
		return Connection{}, err
	}
	refreshPlain, refreshSealed, err := r.seal(userID, refresh)
	if err != nil {
		return Connection{}, err
	}

	row, err := r.q.UpsertStravaConnection(ctx, stravadb.UpsertStravaConnectionParams{
		UserID:             userID,
		AthleteID:          athleteID,
		AccessToken:        accessPlain,
		RefreshToken:       refreshPlain,
		AccessTokenSealed:  accessSealed,
		RefreshTokenSealed: refreshSealed,
		ExpiresAt:          expiresAt,
		Scopes:             scopes,
	})
	if err != nil {
		// Deliberately not wrapping with the parameters: they include the
		// tokens.
		return Connection{}, apperr.Wrap(err, "save strava connection")
	}
	return r.fromDB(row)
}

func (r *Repository) UpdateTokens(ctx context.Context, userID uuid.UUID, access, refresh string, expiresAt time.Time) (Connection, error) {
	accessPlain, accessSealed, err := r.seal(userID, access)
	if err != nil {
		return Connection{}, err
	}
	refreshPlain, refreshSealed, err := r.seal(userID, refresh)
	if err != nil {
		return Connection{}, err
	}

	row, err := r.q.UpdateStravaTokens(ctx, stravadb.UpdateStravaTokensParams{
		UserID:             userID,
		AccessToken:        accessPlain,
		RefreshToken:       refreshPlain,
		AccessTokenSealed:  accessSealed,
		RefreshTokenSealed: refreshSealed,
		ExpiresAt:          expiresAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, apperr.ErrNotFound
		}
		return Connection{}, apperr.Wrap(err, "update strava tokens")
	}
	return r.fromDB(row)
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
	return r.fromDB(row)
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

// RouteTotals sums distance and climb over a half-open window.
//
// A quiet week is a zero struct, not apperr.ErrNotFound: "they rode nothing"
// is an answer, and the caller renders it by saying nothing rather than by
// handling an error.
func (r *Repository) RouteTotals(ctx context.Context, userID uuid.UUID, since, until time.Time) (activity.RouteTotals, error) {
	row, err := r.q.SumStravaActivitiesBetween(ctx, stravadb.SumStravaActivitiesBetweenParams{
		UserID: userID,
		Since:  since,
		Until:  until,
	})
	if err != nil {
		return activity.RouteTotals{}, apperr.Wrap(err, "sum strava activities")
	}

	return activity.RouteTotals{
		Activities: int(row.Activities),
		DistanceM:  row.DistanceM,
		ElevationM: row.ElevationM,
	}, nil
}

// fromDB is a method rather than a function because decrypting needs the
// sealer, and the tokens must not leave this layer still encrypted.
func (r *Repository) fromDB(row stravadb.StravaConnection) (Connection, error) {
	access, err := r.open(row.UserID, row.AccessToken, row.AccessTokenSealed)
	if err != nil {
		return Connection{}, err
	}
	refresh, err := r.open(row.UserID, row.RefreshToken, row.RefreshTokenSealed)
	if err != nil {
		return Connection{}, err
	}

	return Connection{
		UserID:       row.UserID.String(),
		AthleteID:    row.AthleteID,
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    row.ExpiresAt,
		Scopes:       row.Scopes,
		LastSyncedAt: row.LastSyncedAt,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}
