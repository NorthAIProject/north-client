-- +goose Up
-- +goose StatementBegin

-- How much of an expensive action one account has spent in the window it is
-- currently in.
--
-- In Postgres rather than in memory, which is where the bearer surfaces keep
-- their buckets. Those bound a flood of cheap requests and want the check to
-- cost nothing; these bound a paid model call, so one small write alongside it
-- is free — and a counter in a process is wrong the moment a second replica
-- exists, which is exactly when a spend limit starts to matter.
CREATE TABLE quota_counters (
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- What is being counted: 'coach_message', 'document_upload',
    -- 'report_generate'. Deliberately unconstrained, for the same reason
    -- health_metrics.metric is: a new guarded action should need a line of Go,
    -- not a migration before it can refuse anything.
    action       text        NOT NULL,

    -- The floor of the current window, so every request in the same window
    -- lands on the same row and the primary key does the counting. Storing an
    -- expiry instead would need a read to decide which row to write.
    window_start timestamptz NOT NULL,

    used         integer     NOT NULL DEFAULT 0,

    PRIMARY KEY (user_id, action, window_start)
);

-- Serves the sweep, which is the only query that does not know a user_id and so
-- the only one the primary key cannot answer. Without it the sweep degrades
-- into a sequential scan over every window every account has ever opened.
CREATE INDEX quota_counters_window_start_idx ON quota_counters (window_start);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE quota_counters;
-- +goose StatementEnd
