-- name: UpsertHealthMetric :one
-- Ingest is replay-safe by construction: a phone bridge re-sends overlapping
-- windows every sync, so the second arrival of a reading updates it rather
-- than duplicating it. The value is overwritten because a provider that
-- restates a reading is correcting it.
INSERT INTO health_metrics (user_id, source, metric, value, unit, started_at, ended_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, source, metric, started_at) DO UPDATE
SET value      = EXCLUDED.value,
    unit       = EXCLUDED.unit,
    ended_at   = EXCLUDED.ended_at,
    updated_at = now()
RETURNING *;

-- name: LatestHealthMetric :one
SELECT * FROM health_metrics
WHERE user_id = $1 AND metric = $2
ORDER BY started_at DESC
LIMIT 1;

-- name: ListHealthMetricsBetween :many
-- Half-open [since, until), so this window and the one before it can be
-- compared without double-counting the reading on the boundary.
SELECT * FROM health_metrics
WHERE user_id = $1 AND metric = $2
  AND started_at >= $3 AND started_at < $4
ORDER BY started_at DESC;

-- name: AverageHealthMetricBetween :one
-- Returns NULL when the window holds no readings, which the caller must treat
-- as "not measured" rather than zero: a resting heart rate of 0 is a very
-- different claim from an absent one.
SELECT AVG(value)::double precision AS average
FROM health_metrics
WHERE user_id = $1 AND metric = $2
  AND started_at >= $3 AND started_at < $4;

-- name: SumHealthMetricBetween :one
-- For the counting metrics — steps, active calories — where the total over a
-- window is the meaningful number, not the average.
SELECT COALESCE(SUM(value), 0)::double precision AS total
FROM health_metrics
WHERE user_id = $1 AND metric = $2
  AND started_at >= $3 AND started_at < $4;

-- name: ListHealthMetricSources :many
-- Which providers have written for this user, and how recently. Drives the
-- connection status shown on the fitness page without a per-provider query.
SELECT source, COUNT(*) AS reading_count, MAX(started_at)::timestamptz AS last_reading_at
FROM health_metrics
WHERE user_id = $1
GROUP BY source
ORDER BY source;

-- name: DeleteHealthMetricsBySource :exec
-- Disconnecting a provider takes its readings with it. North keeps history it
-- was given, but a person revoking a source is an explicit request to forget.
DELETE FROM health_metrics
WHERE user_id = $1 AND source = $2;
