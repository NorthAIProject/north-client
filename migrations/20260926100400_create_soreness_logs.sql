-- +goose Up
-- +goose StatementBegin

-- Where it hurts today. One row per region per day, corrected in place: you
-- do not have two sore backs. region is validated in Go
-- (internal/soreness/soreness), so a new region is a code change.
CREATE TABLE soreness_logs (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    log_date  date        NOT NULL,

    region    text        NOT NULL,
    -- 1 stiff, 2 sore, 3 painful.
    severity  smallint    NOT NULL CHECK (severity BETWEEN 1 AND 3),
    note      text        NOT NULL DEFAULT '',

    logged_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, log_date, region)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE soreness_logs;
-- +goose StatementEnd
