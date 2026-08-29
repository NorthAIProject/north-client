-- +goose Up
-- +goose StatementBegin

-- Why a sync did not happen.
--
-- Until now a failed sync was indistinguishable from one that never ran, and
-- both were indistinguishable from a fresh connection: the fitness card reads
-- last_synced_at, and a NULL there renders "No sync yet." forever. That is the
-- state a real account sat in for eleven days while four sync_strava jobs
-- waited in the queue for a worker process nobody had started, and nothing in
-- the interface could have told anyone.
--
-- Two columns rather than one. last_sync_attempted_at moves on every run,
-- successful or not, so "we tried and it broke" is distinguishable from "we
-- have not tried"; last_synced_at keeps its existing meaning as the watermark
-- the next import window starts from, and must not advance on failure.
--
-- The error is NOT NULL DEFAULT '' rather than nullable: it is cleared on every
-- success, and empty is the same statement as absent for a string nobody joins
-- on. It holds a wrapped Go error — the client discards Strava response bodies
-- and reports status codes only, so no token material reaches this column.
ALTER TABLE strava_connections
    ADD COLUMN last_sync_error text NOT NULL DEFAULT '',
    ADD COLUMN last_sync_attempted_at timestamptz;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE strava_connections
    DROP COLUMN IF EXISTS last_sync_attempted_at,
    DROP COLUMN IF EXISTS last_sync_error;
-- +goose StatementEnd
