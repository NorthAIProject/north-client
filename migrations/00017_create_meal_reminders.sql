-- +goose Up
-- +goose StatementBegin

CREATE TABLE meal_reminders (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    label                 text        NOT NULL,
    -- "HH:MM", 24-hour, zero-padded. Stored as text rather than Postgres's
    -- `time` type: this is a display/comparison string, not a value that
    -- needs interval arithmetic, and it keeps the Go side a plain string
    -- instead of pgtype.Time's microseconds-since-midnight representation.
    time_of_day           text        NOT NULL CHECK (time_of_day ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    -- 0=Sunday .. 6=Saturday, matching Go's time.Weekday.
    days_of_week          smallint[]  NOT NULL DEFAULT '{0,1,2,3,4,5,6}',
    enabled               boolean     NOT NULL DEFAULT true,

    -- Marked when DueNow returns this reminder, so the same reminder does
    -- not fire twice in one local day. Same idempotency idiom as check_ins'
    -- UNIQUE(user_id, local_date), applied per-reminder instead of per-day.
    last_fired_local_date date,

    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX meal_reminders_user_idx ON meal_reminders (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE meal_reminders;
-- +goose StatementEnd
