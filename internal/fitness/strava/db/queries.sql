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
