-- +goose NO TRANSACTION
-- +goose Up

-- Finds a person's recently failed job of one kind.
--
-- The embed sweep asks this for each person missing vectors, so that one bad
-- passage stops being retried every fifteen minutes. Without an index the fix
-- would scan the whole of jobs once per person, which is the problem
-- jobs_active_kind_user_idx exists to remove.
--
-- Only failed rows enter it, and those are few, so it costs next to nothing to
-- keep. The drop is there for a retry; see jobs_active_kind_user_idx.
DROP INDEX CONCURRENTLY IF EXISTS jobs_failed_kind_user_idx;

CREATE INDEX CONCURRENTLY jobs_failed_kind_user_idx
    ON jobs (kind, (payload->>'user_id'), updated_at)
    WHERE status = 'failed';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS jobs_failed_kind_user_idx;
