-- +goose NO TRANSACTION
-- +goose Up

-- At most one live copy of each periodic sweep.
--
-- The worker enqueued a sweep on every tick whatever was already queued, so a
-- stalled lane built up a backlog of the same sweep. This index, with ON CONFLICT
-- DO NOTHING in EnqueuePeriodicJob, makes the check and the insert one step.
--
-- A sweep is the job with an empty payload: nothing else in the code enqueues
-- one, and an empty payload names no person, document or message, so a second
-- copy holds no work the first does not.
--
-- Running counts as well as pending, and that is load-bearing. Retrying,
-- requeueing and releasing a stale job all move a row from running back to
-- pending. With an index on pending alone, a tick arriving during a run would
-- make that move a unique violation. For a stale release it would be worse: that
-- is one statement over every abandoned job, so one collision would stop all of
-- them being released, whatever their kind. Here a row keeps its own slot through
-- those moves, so only an insert can ever conflict.
--
-- The drop is there for a retry; see jobs_active_kind_user_idx.
DROP INDEX CONCURRENTLY IF EXISTS jobs_periodic_active_kind_idx;

-- Copies already in the queue would fail the build below, and the moment of any
-- deploy can have some: a sweep running with its next tick queued is normal.
--
-- They are closed rather than deleted, so what happened stays readable. The row
-- kept is a running one if there is one, since a worker may be partway through it,
-- then the most recently touched. Closing a row a worker is still running does no
-- harm: its finish overwrites the status, and a retry that would collide fails
-- and leaves it closed, for the next tick to cover.
--
-- The worker from the previous release is still up at this point. If a tick lands
-- between this and the build, the build fails, the migration reports it, and
-- running it again starts clean from the drop above.
UPDATE jobs
SET status     = 'done',
    last_error = 'superseded: duplicate periodic job closed when jobs_periodic_active_kind_idx was built'
                 || CASE WHEN last_error = '' THEN '' ELSE ' (last error: ' || last_error || ')' END,
    updated_at = now()
WHERE id IN (
    SELECT id
    FROM (
        SELECT id,
               row_number() OVER (
                   PARTITION BY kind
                   ORDER BY (status = 'running') DESC, updated_at DESC, id
               ) AS copy
        FROM jobs
        WHERE status IN ('pending', 'running')
          AND payload = '{}'::jsonb
    ) AS copies
    WHERE copy > 1
);

CREATE UNIQUE INDEX CONCURRENTLY jobs_periodic_active_kind_idx
    ON jobs (kind)
    WHERE status IN ('pending', 'running') AND payload = '{}'::jsonb;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS jobs_periodic_active_kind_idx;
