-- name: UpsertStravaConnection :one
INSERT INTO strava_connections (
    user_id, athlete_id, access_token, refresh_token, expires_at, scopes
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (user_id) DO UPDATE SET
    athlete_id    = EXCLUDED.athlete_id,
    access_token  = EXCLUDED.access_token,
    refresh_token = EXCLUDED.refresh_token,
    expires_at    = EXCLUDED.expires_at,
    scopes        = EXCLUDED.scopes,
    updated_at    = now()
RETURNING *;

-- name: UpdateStravaTokens :one
UPDATE strava_connections
SET access_token = $2, refresh_token = $3, expires_at = $4, updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: MarkStravaSynced :exec
UPDATE strava_connections
SET last_synced_at = $2, updated_at = now()
WHERE user_id = $1;

-- name: GetStravaConnection :one
SELECT * FROM strava_connections WHERE user_id = $1;

-- name: DeleteStravaConnection :exec
DELETE FROM strava_connections WHERE user_id = $1;

-- name: UpsertStravaActivity :exec
INSERT INTO strava_activities (
    user_id, strava_id, name, sport_type, start_date,
    distance_m, moving_time_s, elapsed_time_s,
    total_elevation_gain_m, average_speed_ms, summary_polyline
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
ON CONFLICT (user_id, strava_id) DO UPDATE SET
    name                   = EXCLUDED.name,
    sport_type             = EXCLUDED.sport_type,
    start_date             = EXCLUDED.start_date,
    distance_m             = EXCLUDED.distance_m,
    moving_time_s          = EXCLUDED.moving_time_s,
    elapsed_time_s         = EXCLUDED.elapsed_time_s,
    total_elevation_gain_m = EXCLUDED.total_elevation_gain_m,
    average_speed_ms       = EXCLUDED.average_speed_ms,
    summary_polyline       = EXCLUDED.summary_polyline,
    updated_at             = now();

-- name: ListStravaActivities :many
SELECT * FROM strava_activities
WHERE user_id = $1
ORDER BY start_date DESC
LIMIT $2;
