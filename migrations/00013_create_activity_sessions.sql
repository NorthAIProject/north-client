-- +goose Up
-- +goose StatementBegin

CREATE TABLE activity_sessions (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Validated against a Go-owned MET table (internal/activity/activity),
    -- not a foreign key: the table is a small curated const list, not
    -- something that needs its own row lifecycle yet.
    activity_code        text        NOT NULL,

    -- 'manual' today; 'strava' is a seam for a future sync to write through,
    -- not something this migration wires up.
    source                text        NOT NULL DEFAULT 'manual'
                                      CHECK (source IN ('manual', 'strava')),

    status               text        NOT NULL DEFAULT 'active'
                                      CHECK (status IN ('active', 'paused', 'completed', 'cancelled')),

    -- Captured at start, so a later weight change never rewrites an old
    -- session's calorie burn.
    weight_kg_snapshot   double precision NOT NULL,

    started_at           timestamptz NOT NULL DEFAULT now(),
    paused_at            timestamptz,
    total_paused_seconds integer     NOT NULL DEFAULT 0,
    ended_at             timestamptz,
    calories_burned      double precision,

    -- Room for a future Strava sync to dedupe imports; unused by manual
    -- sessions.
    external_id          text,

    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- At most one open (active or paused) session per user at a time.
CREATE UNIQUE INDEX activity_sessions_one_open_uidx ON activity_sessions (user_id) WHERE status IN ('active', 'paused');
CREATE INDEX activity_sessions_user_started_idx ON activity_sessions (user_id, started_at DESC);
CREATE UNIQUE INDEX activity_sessions_external_uidx ON activity_sessions (source, external_id) WHERE external_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE activity_sessions;
-- +goose StatementEnd
