-- name: AddSnapshot :exec
-- A forecast is recorded once. A second run in the same week changes nothing.
INSERT INTO horizon_snapshots (user_id, made_on, series_key, horizon_weeks, low, mid, high)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, series_key, horizon_weeks, made_on) DO NOTHING;

-- name: ListSnapshots :many
SELECT made_on, series_key, horizon_weeks, low, mid, high
FROM horizon_snapshots
WHERE user_id = $1 AND horizon_weeks = $2 AND made_on >= $3
ORDER BY series_key, made_on;

-- name: LatestSnapshotDay :one
SELECT COALESCE(MAX(made_on), '0001-01-01'::date)::date AS made_on
FROM horizon_snapshots
WHERE user_id = $1;
