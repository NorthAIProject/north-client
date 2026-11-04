-- name: EnqueueJob :one
INSERT INTO jobs (kind, payload, run_after, max_attempts, request_id)
VALUES ($1, $2, coalesce(sqlc.narg('run_after')::timestamptz, now()), $3, sqlc.narg('request_id'))
RETURNING *;

-- name: ClaimJob :one
-- Claims one eligible job for this worker.
--
-- FOR UPDATE SKIP LOCKED is what makes a plain table into a queue: concurrent
-- workers step over each other's locked rows instead of blocking, so running a
-- second worker doubles throughput rather than serialising it.
UPDATE jobs
SET status     = 'running',
    attempts   = attempts + 1,
    updated_at = now()
WHERE id = (
    SELECT id FROM jobs
    WHERE status = 'pending' AND run_after <= now()
    ORDER BY run_after
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: CompleteJob :exec
UPDATE jobs
SET status = 'done', updated_at = now()
WHERE id = $1;

-- name: RetryJob :exec
-- Returns a failed job to the queue with a later eligibility time.
UPDATE jobs
SET status     = 'pending',
    run_after  = $2,
    last_error = $3,
    updated_at = now()
WHERE id = $1;

-- name: RequeueInterruptedJob :exec
-- Returns a job a shutdown interrupted, ready to run at once and with the attempt
-- given back: a deploy says nothing about whether the job can succeed.
UPDATE jobs
SET status     = 'pending',
    attempts   = greatest(attempts - 1, 0),
    run_after  = now(),
    last_error = $2,
    updated_at = now()
WHERE id = $1;

-- name: FailJob :exec
UPDATE jobs
SET status = 'failed', last_error = $2, updated_at = now()
WHERE id = $1;

-- name: RequeueFailedEmbedJobsForUser :execrows
-- The parameter carries its own ::uuid cast. Without one sqlc cannot infer a
-- type through the payload cast on the left and falls back to []byte, which
-- pgx then sends as bytea — the comparison matches nothing and the update
-- silently requeues zero rows.
UPDATE jobs
SET status     = 'pending',
    attempts   = 0,
    run_after  = now(),
    last_error = '',
    updated_at = now()
WHERE kind = 'embed_chunks'
  AND status = 'failed'
  AND (payload->>'user_id')::uuid = sqlc.arg(user_id)::uuid;

-- name: HasPendingJobForUser :one
-- Compares text to text, unlike RequeueFailedEmbedJobsForUser above. A cast on
-- the column hides it from jobs_active_kind_user_idx, which indexes the text
-- value, and this runs once per candidate user in four sweeps.
SELECT EXISTS(
    SELECT 1 FROM jobs
    WHERE kind = sqlc.arg(kind)::text
      AND status IN ('pending', 'running')
      AND payload->>'user_id' = sqlc.arg(user_id)::text
)::bool;

-- name: HasRecentFailedJobForUser :one
-- The cutoff is computed here for the reason ReleaseStaleJobs gives: updated_at
-- is the database's clock, so the comparison should not depend on ours agreeing.
SELECT EXISTS(
    SELECT 1 FROM jobs
    WHERE kind = sqlc.arg(kind)::text
      AND status = 'failed'
      AND payload->>'user_id' = sqlc.arg(user_id)::text
      AND updated_at > now() - make_interval(secs => sqlc.arg('within_seconds')::double precision)
)::bool;

-- name: ReleaseStaleJobs :execrows
-- Returns jobs abandoned by a worker that died mid-run. Without this a crash
-- leaves them 'running' forever and the work silently never happens.
--
-- The cutoff is computed here rather than passed in as a Go timestamp. updated_at
-- is set by the database, so comparing it against the application's clock makes
-- the sweep depend on the two agreeing — and with Postgres in a container they
-- do not have to.
--
-- A job abandoned on its last attempt is failed rather than requeued. Claiming
-- counts the attempt, so requeueing it would run it past max_attempts, and a
-- job whose input always outlives its worker would come back forever.
UPDATE jobs
SET status     = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
    last_error = CASE WHEN attempts < max_attempts THEN last_error
                      ELSE 'abandoned while running on its last attempt'
                           || CASE WHEN last_error = '' THEN '' ELSE '; last error: ' || last_error END
                 END,
    updated_at = now()
WHERE status = 'running'
  AND updated_at < now() - make_interval(secs => sqlc.arg('stale_seconds')::double precision);
