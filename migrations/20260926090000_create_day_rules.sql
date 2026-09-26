-- +goose Up
-- +goose StatementBegin

-- A day rule is a standing intention about the shape of a day: when the kitchen
-- closes, the last caffeine, the last drink, screens off. It is an objective in
-- DOMAIN.md's terms — a target the day is read against, never an event — so it
-- is one row per kind rather than a log.
--
-- kind is plain text validated in Go (internal/day/day), like life domains: a
-- new kind is a code change, never a migration.
CREATE TABLE day_rules (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL,

    -- "HH:MM", 24-hour, zero-padded — the same convention as sleep_logs and
    -- meal_reminders.
    at_time    text        NOT NULL CHECK (at_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),

    enabled    boolean     NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, kind)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE day_rules;
-- +goose StatementEnd
