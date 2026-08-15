-- +goose Up
-- +goose StatementBegin

-- One table for every scalar health reading, rather than one table per metric.
-- Heart rate, HRV, VO2max, SpO2, sleep stages, daily step counts and body
-- composition all have the same shape: a number, a unit, and a window of time
-- it applies to. Splitting them apart would buy nothing and cost a migration
-- every time a provider exposes one more field.
CREATE TABLE health_metrics (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Where the reading came from: 'apple_health', 'strava', 'whoop', 'manual'.
    -- Deliberately unconstrained. activity_sessions.source carries a CHECK
    -- enum, which means every new provider needs a migration before it can
    -- write a single row; that is the mistake this table is not repeating.
    source       text        NOT NULL,

    -- What was measured: 'heart_rate', 'hrv_sdnn', 'vo2max', 'spo2', 'steps',
    -- 'sleep_deep', 'body_fat_pct'. Also unconstrained, for the same reason.
    metric       text        NOT NULL,

    value        double precision NOT NULL,

    -- Carried rather than assumed, because the same metric arrives in
    -- different units from different providers ('bpm', 'ms', 'count',
    -- 'seconds', 'percent'). Normalising on write would silently destroy the
    -- provider's own answer.
    unit         text        NOT NULL,

    -- Half-open [started_at, ended_at). A NULL ended_at means an instantaneous
    -- sample — a single heart-rate beat — as opposed to an interval like a
    -- deep-sleep block or a day's step total.
    started_at   timestamptz NOT NULL,
    ended_at     timestamptz,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT health_metrics_window_ck CHECK (ended_at IS NULL OR ended_at > started_at)
);

-- The natural key, and the conflict target for ingest. A phone bridge re-sends
-- overlapping windows on every sync, so the same reading arrives many times;
-- this is what makes replaying a payload a no-op instead of a duplicate.
--
-- Note it is scoped by user_id. activity_sessions dedupes on (source,
-- external_id) alone, which is only safe because Strava activity IDs happen to
-- be globally unique. Readings pushed from a phone carry no such guarantee.
CREATE UNIQUE INDEX health_metrics_natural_uidx
    ON health_metrics (user_id, source, metric, started_at);

-- Serves the read the coach actually makes: one metric, recent first,
-- for one person.
CREATE INDEX health_metrics_user_metric_idx
    ON health_metrics (user_id, metric, started_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE health_metrics;
-- +goose StatementEnd
