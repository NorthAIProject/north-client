-- name: CreateActivitySession :one
INSERT INTO activity_sessions (user_id, activity_code, weight_kg_snapshot, plan_weekday)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(activity_code),
    sqlc.arg(weight_kg_snapshot),
    sqlc.narg(plan_weekday)
)
RETURNING *;

-- name: GetActivitySession :one
SELECT * FROM activity_sessions WHERE id = $1 AND user_id = $2;

-- name: ActiveActivitySession :one
SELECT * FROM activity_sessions WHERE user_id = $1 AND status IN ('active', 'paused');

-- name: PauseActivitySession :one
UPDATE activity_sessions
SET status = 'paused', paused_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: ResumeActivitySession :one
UPDATE activity_sessions
SET status = 'active', paused_at = NULL, total_paused_seconds = $3, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CompleteActivitySession :one
UPDATE activity_sessions
SET status = 'completed', ended_at = $3, calories_burned = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CancelActivitySession :exec
UPDATE activity_sessions
SET status = 'cancelled', ended_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: ListActivitySessions :many
SELECT * FROM activity_sessions
WHERE user_id = $1
ORDER BY started_at DESC
LIMIT $2;

-- name: SumActivityCaloriesSince :one
SELECT COALESCE(SUM(calories_burned), 0)::double precision AS total
FROM activity_sessions
WHERE user_id = $1 AND status = 'completed' AND ended_at >= $2;

-- name: SumActivityCaloriesBetween :one
-- Half-open [since, until), so this window and the one before it can be
-- compared without double-counting the session on the boundary.
SELECT COALESCE(SUM(calories_burned), 0)::double precision AS total
FROM activity_sessions
WHERE user_id = $1 AND status = 'completed'
  AND ended_at >= $2 AND ended_at < $3;

-- name: ListActivitySessionsBetween :many
-- Completed sessions only: an abandoned or still-running session is not
-- something that happened.
SELECT * FROM activity_sessions
WHERE user_id = $1 AND status = 'completed'
  AND ended_at >= $2 AND ended_at < $3
ORDER BY ended_at DESC;

-- name: ImportActivitySession :one
-- A finished session written in one shot, for a provider sync rather than
-- the in-app start/stop lifecycle. Deduped by the existing
-- UNIQUE (source, external_id) index, so re-importing is a no-op.
INSERT INTO activity_sessions (
    user_id, activity_code, source, status, weight_kg_snapshot,
    started_at, ended_at, calories_burned, external_id,
    distance_m, avg_hr, max_hr, elevation_m, indoor
) VALUES (
    $1, $2, $3, 'completed', $4, $5, $6, $7, $8,
    sqlc.narg(distance_m), sqlc.narg(avg_hr), sqlc.narg(max_hr), sqlc.narg(elevation_m), sqlc.narg(indoor)
)
ON CONFLICT (source, external_id) WHERE external_id IS NOT NULL DO NOTHING
RETURNING *;

-- name: FillImportedActivitySession :exec
-- What a re-import knows that the first import did not: a provider that
-- started sending heart rate, or an older row from before these columns.
-- Only empty fields are filled; nothing already recorded is overwritten.
UPDATE activity_sessions
SET distance_m  = COALESCE(distance_m, sqlc.narg(distance_m)),
    avg_hr      = COALESCE(avg_hr, sqlc.narg(avg_hr)),
    max_hr      = COALESCE(max_hr, sqlc.narg(max_hr)),
    elevation_m = COALESCE(elevation_m, sqlc.narg(elevation_m)),
    indoor      = COALESCE(indoor, sqlc.narg(indoor)),
    updated_at  = now()
WHERE user_id = sqlc.arg(user_id)
  AND source = sqlc.arg(source)
  AND external_id = sqlc.arg(external_id);

-- name: LogActivitySession :one
-- A finished session written in one shot by the person who did it, rather
-- than a provider. No external id: there is nothing to dedupe against, and
-- two identical runs on the same day are two runs.
INSERT INTO activity_sessions (
    user_id, activity_code, source, status, weight_kg_snapshot,
    started_at, ended_at, calories_burned, distance_m
) VALUES (
    $1, $2, 'manual', 'completed', $3, $4, $5, $6, $7
)
RETURNING *;
