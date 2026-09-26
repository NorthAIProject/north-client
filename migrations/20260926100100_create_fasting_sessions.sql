-- +goose Up
-- +goose StatementBegin

-- A fast is a window with a start and, eventually, an end. It is a log in
-- DOMAIN.md's terms — it happened — but it spans time the way an activity
-- session does, so it is shaped like one.
CREATE TABLE fasting_sessions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    started_at   timestamptz NOT NULL,
    ended_at     timestamptz CHECK (ended_at IS NULL OR ended_at > started_at),

    target_hours smallint    NOT NULL DEFAULT 16 CHECK (target_hours BETWEEN 1 AND 72),

    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX fasting_sessions_user_started_idx ON fasting_sessions (user_id, started_at DESC);

-- At most one open fast per person. Starting a second is a mistake the
-- database refuses rather than one the page has to explain.
CREATE UNIQUE INDEX fasting_sessions_one_open_idx ON fasting_sessions (user_id) WHERE ended_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE fasting_sessions;
-- +goose StatementEnd
