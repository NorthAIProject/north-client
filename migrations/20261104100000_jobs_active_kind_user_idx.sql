-- +goose NO TRANSACTION
-- +goose Up

-- Finds a person's queued or running job of one kind.
--
-- The Strava status on the fitness hub and four sweeps ask this, the sweeps once
-- per candidate user, and until now each ask scanned every job ever written.
--
-- Only pending and running rows enter the index, so it stays the size of the
-- queue rather than the size of the table's history.
--
-- Concurrently, because the worker polls this table and a plain build would
-- block every enqueue for as long as it took. That needs NO TRANSACTION.
--
-- The drop is for a retry. A concurrent build that fails leaves an invalid index
-- behind under this name, goose records nothing, and the next attempt would fail
-- on "already exists" forever. IF NOT EXISTS would be worse: it would accept the
-- invalid index and report success.
DROP INDEX CONCURRENTLY IF EXISTS jobs_active_kind_user_idx;

CREATE INDEX CONCURRENTLY jobs_active_kind_user_idx
    ON jobs (kind, (payload->>'user_id'))
    WHERE status IN ('pending', 'running');

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS jobs_active_kind_user_idx;
