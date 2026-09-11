-- Both forms of each token are written together, and exactly one of each pair
-- carries a value: sealed when the deployment has an encryption key, plaintext
-- when it does not. Writing both clears whichever the previous write used, so a
-- row that was plaintext before a key was configured does not keep a readable
-- copy alongside the sealed one.
-- name: UpsertStravaConnection :one
INSERT INTO strava_connections (
    user_id, athlete_id,
    access_token, refresh_token,
    access_token_sealed, refresh_token_sealed,
    expires_at, scopes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (user_id) DO UPDATE SET
    athlete_id           = EXCLUDED.athlete_id,
    access_token         = EXCLUDED.access_token,
    refresh_token        = EXCLUDED.refresh_token,
    access_token_sealed  = EXCLUDED.access_token_sealed,
    refresh_token_sealed = EXCLUDED.refresh_token_sealed,
    expires_at           = EXCLUDED.expires_at,
    scopes               = EXCLUDED.scopes,
    updated_at           = now()
RETURNING *;

-- name: UpdateStravaTokens :one
UPDATE strava_connections
SET access_token         = $2,
    refresh_token        = $3,
    access_token_sealed  = $4,
    refresh_token_sealed = $5,
    expires_at           = $6,
    updated_at           = now()
WHERE user_id = $1
RETURNING *;

-- Success clears the error as well as moving the watermark: a card that keeps
-- showing yesterday's failure after today's sync worked is worse than one that
-- shows nothing.
-- name: MarkStravaSynced :exec
UPDATE strava_connections
SET last_synced_at         = $2,
    last_sync_attempted_at = now(),
    last_sync_error        = '',
    updated_at             = now()
WHERE user_id = $1;

-- last_synced_at is deliberately untouched. It is the window the next import
-- starts from, and advancing it on a failure would skip whatever that run
-- never managed to fetch.
-- name: MarkStravaSyncFailed :exec
UPDATE strava_connections
SET last_sync_attempted_at = now(),
    last_sync_error        = $2,
    updated_at             = now()
WHERE user_id = $1;

-- Connections the periodic sweep should re-sync: never synced, or not since
-- the cutoff. Ordered oldest first so a backlog drains fairly rather than
-- starving whoever sorts last by id.
-- name: ListStravaConnectionsDueForSync :many
SELECT user_id FROM strava_connections
WHERE last_synced_at IS NULL
   OR last_synced_at < sqlc.arg(before)::timestamptz
ORDER BY last_synced_at ASC NULLS FIRST
LIMIT sqlc.arg(max_rows)::int;

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

-- name: ListStravaActivitiesBetween :many
-- Half-open window in absolute time, matching SumStravaActivitiesBetween.
--
-- The caller has already turned local week boundaries into instants using the
-- reader's own location, so this query knows nothing about timezones and
-- cannot disagree with the bucketing that happens above it.
--
-- Ascending, unlike a "most recent first" list: the terrain builder walks
-- weeks in the order it lays them out, and sorting the same rows twice to get
-- there would be silly. The (user_id, start_date DESC) index still serves
-- this — Postgres reads an index backwards as happily as forwards.
SELECT * FROM strava_activities
WHERE user_id = $1
  AND start_date >= sqlc.arg(since)::timestamptz
  AND start_date <  sqlc.arg(until)::timestamptz
ORDER BY start_date ASC;

-- name: OldestStravaActivityBefore :one
-- The start of the oldest activity older than the cursor, if there is one.
--
-- This is what stops the terrain asking for another page forever. Returning a
-- row or not is the whole signal: no row means no more ground, which the
-- repository turns into a nil *time.Time.
--
-- Deliberately not count(*) + min(). Both would read every older row to
-- answer "is there anything older", where this reads exactly one index entry
-- — the (user_id, start_date DESC) index scanned backwards. It also sidesteps
-- min() over the empty set being NULL, which sqlc types as non-nullable once
-- the aggregate is cast and pgx then refuses to scan.
SELECT start_date FROM strava_activities
WHERE user_id = $1
  AND start_date < sqlc.arg(before)::timestamptz
ORDER BY start_date ASC
LIMIT 1;

-- name: SumStravaActivitiesBetween :one
-- Half-open window, matching timerange.Range. Both summed columns are NOT NULL,
-- so the coalesce only guards the empty-set case, where sum() returns NULL.
SELECT
    count(*)::bigint                                           AS activities,
    coalesce(sum(distance_m), 0)::double precision             AS distance_m,
    coalesce(sum(total_elevation_gain_m), 0)::double precision AS elevation_m
FROM strava_activities
WHERE user_id = $1
  AND start_date >= sqlc.arg(since)::timestamptz
  AND start_date <  sqlc.arg(until)::timestamptz;
