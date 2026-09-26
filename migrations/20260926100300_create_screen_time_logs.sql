-- +goose Up
-- +goose StatementBegin

-- A day's screen time: upsert-per-day, like sleep_logs. Typed in, or posted by
-- a Shortcut — Apple does not let an app read the number and send it anywhere.
CREATE TABLE screen_time_logs (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    local_date date        NOT NULL,

    minutes    integer     NOT NULL CHECK (minutes >= 0 AND minutes <= 1440),
    source     text        NOT NULL DEFAULT 'manual',

    updated_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, local_date)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE screen_time_logs;
-- +goose StatementEnd
