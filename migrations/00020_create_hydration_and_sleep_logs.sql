-- +goose Up
-- +goose StatementBegin

-- Water arrives a glass at a time, so this is an append log aggregated on
-- read — the same shape as food_logs, not one row per day. log_date is
-- computed in the user's timezone at write time, so "today" is their today.
CREATE TABLE hydration_logs (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    log_date  date        NOT NULL,

    -- Upper bound is a typo guard, not a health opinion: it catches someone
    -- entering litres in a millilitre field.
    amount_ml integer     NOT NULL CHECK (amount_ml > 0 AND amount_ml <= 5000),

    logged_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX hydration_logs_user_date_idx ON hydration_logs (user_id, log_date DESC);

-- A night's sleep happens once and gets corrected rather than repeated, so
-- this is upsert-per-day like check_ins. local_date is the morning they woke
-- up, which is the day the sleep counts toward.
CREATE TABLE sleep_logs (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    local_date       date        NOT NULL,

    duration_minutes integer     NOT NULL CHECK (duration_minutes > 0 AND duration_minutes <= 1440),

    -- Nullable: logging that you slept six hours is useful on its own, and
    -- demanding a rating is how a tracker stops getting used. Same 1-5 scale
    -- as check_ins.mood so the two read consistently side by side.
    quality          smallint    CHECK (quality BETWEEN 1 AND 5),

    -- "HH:MM", 24-hour, zero-padded. text rather than Postgres `time` for the
    -- same reason as meal_reminders.time_of_day: these are display and
    -- comparison strings, not values needing interval arithmetic, and it
    -- keeps the Go side a plain string instead of pgtype.Time.
    bedtime          text        CHECK (bedtime ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    wake_time        text        CHECK (wake_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),

    notes            text        NOT NULL DEFAULT '',

    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, local_date)
);

CREATE INDEX sleep_logs_user_date_idx ON sleep_logs (user_id, local_date DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sleep_logs;
DROP TABLE hydration_logs;
-- +goose StatementEnd
