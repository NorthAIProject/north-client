-- +goose Up
-- +goose StatementBegin

-- How often North asks for something on a cadence the person chose.
-- One row per (user, kind). Kinds are validated in Go.
CREATE TABLE user_alert_schedules (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind           text        NOT NULL,
    enabled        boolean     NOT NULL DEFAULT true,
    every_days     integer     NOT NULL,
    reminder_days  integer     NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_alert_schedules;
-- +goose StatementEnd
