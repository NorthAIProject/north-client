-- +goose Up
-- +goose StatementBegin

-- Caffeine arrives a cup at a time: append-per-event, like hydration_logs.
-- logged_at matters more here than for water, because what is still active in
-- the body at bedtime depends on when each cup was drunk.
CREATE TABLE caffeine_logs (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    log_date  date        NOT NULL,

    -- Upper bound is a typo guard: no single drink carries a gram of caffeine.
    mg        integer     NOT NULL CHECK (mg > 0 AND mg <= 1000),
    label     text        NOT NULL DEFAULT '',

    logged_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX caffeine_logs_user_logged_idx ON caffeine_logs (user_id, logged_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE caffeine_logs;
-- +goose StatementEnd
