-- name: CreateSetLog :one
-- An activity session that is not the person's own is dropped rather than
-- linked, so a set can never attach to somebody else's workout.
INSERT INTO set_logs (user_id, activity_session_id, log_date, exercise_slug, exercise_name, set_number, weight_kg, reps, performed_at)
VALUES (
    sqlc.arg(user_id),
    (SELECT a.id FROM activity_sessions a WHERE a.id = sqlc.narg(activity_session_id) AND a.user_id = sqlc.arg(user_id)),
    sqlc.arg(log_date), sqlc.arg(exercise_slug), sqlc.arg(exercise_name), sqlc.arg(set_number),
    sqlc.arg(weight_kg), sqlc.arg(reps), sqlc.arg(performed_at)
)
RETURNING *;

-- name: DeleteSetLog :execrows
DELETE FROM set_logs WHERE id = $1 AND user_id = $2;

-- name: ListSetsBetween :many
-- Half-open [since, until) on when the set was done, newest first.
SELECT * FROM set_logs
WHERE user_id = $1 AND performed_at >= $2 AND performed_at < $3
ORDER BY performed_at DESC;

-- name: ListSetsForExercises :many
-- Sets of the given exercises since a point, newest first. The key matches
-- lift.KeyFor: the slug, or the trimmed lower-cased name when there is none.
SELECT * FROM set_logs
WHERE user_id = sqlc.arg(user_id)
  AND performed_at >= sqlc.arg(since)
  AND (CASE WHEN exercise_slug <> '' THEN exercise_slug ELSE lower(btrim(exercise_name)) END) = ANY(sqlc.arg(keys)::text[])
ORDER BY performed_at DESC
LIMIT 500;

-- name: ListSetsForSession :many
-- The sets logged during one timed workout, in the order they were done.
SELECT * FROM set_logs
WHERE user_id = sqlc.arg(user_id) AND activity_session_id = sqlc.arg(activity_session_id)
ORDER BY performed_at, set_number;

-- name: ListSetsForExercisesBefore :many
-- Sets of the given exercises in [since, before), newest first: the history
-- a finished workout is compared against. Keys match lift.KeyFor.
SELECT * FROM set_logs
WHERE user_id = sqlc.arg(user_id)
  AND performed_at >= sqlc.arg(since)
  AND performed_at < sqlc.arg(before)
  AND (CASE WHEN exercise_slug <> '' THEN exercise_slug ELSE lower(btrim(exercise_name)) END) = ANY(sqlc.arg(keys)::text[])
ORDER BY performed_at DESC
LIMIT 500;
