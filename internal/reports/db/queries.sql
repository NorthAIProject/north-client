-- name: CreateReport :one
INSERT INTO reports (user_id, kind, period_start, period_end, title, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetReport :one
SELECT * FROM reports WHERE id = $1 AND user_id = $2;

-- name: GetActiveReportForWeek :one
SELECT * FROM reports
WHERE user_id = $1
  AND kind = $2
  AND period_start = $3
  AND archived_at IS NULL;

-- name: ListReports :many
SELECT * FROM reports
WHERE user_id = $1
  AND (sqlc.arg(include_archived)::boolean OR archived_at IS NULL)
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text)
ORDER BY period_start DESC, created_at DESC
LIMIT $2;

-- name: ArchiveReport :one
UPDATE reports
SET archived_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL
RETURNING *;

-- name: MarkReportPending :one
UPDATE reports
SET status      = 'pending',
    last_error  = '',
    updated_at  = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SaveGeneratedReport :one
UPDATE reports
SET status       = 'ready',
    body         = $3,
    last_error   = '',
    generated_at = now(),
    updated_at   = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: MarkReportFailed :one
UPDATE reports
SET status     = 'failed',
    last_error = $3,
    updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: LatestReadyReport :one
SELECT * FROM reports
WHERE user_id = $1
  AND kind = $2
  AND status = 'ready'
  AND archived_at IS NULL
ORDER BY period_start DESC
LIMIT 1;
