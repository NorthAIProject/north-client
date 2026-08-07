-- +goose Up
-- +goose StatementBegin

-- A habit is a recurring intention: unlike a log it can be missed, which is
-- what makes streaks and adherence mean anything.
CREATE TABLE habits (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    name         text        NOT NULL,

    -- A life domain from internal/shared/lifedomain. text with no CHECK, the
    -- same choice goals.category made: the vocabulary is Go-owned so adding
    -- one is a code change, and existing rows never become invalid.
    domain       text        NOT NULL DEFAULT 'personal',

    -- 0=Sunday .. 6=Saturday, matching Go's time.Weekday and meal_reminders.
    -- Empty is not allowed: a habit scheduled for no days can never be due,
    -- which is a bug rather than a preference.
    days_of_week smallint[]  NOT NULL DEFAULT '{0,1,2,3,4,5,6}'
                             CHECK (array_length(days_of_week, 1) BETWEEN 1 AND 7),

    -- Archived rather than deleted: a habit someone stopped is part of their
    -- history, and the coach knowing they dropped it is the interesting part.
    active       boolean     NOT NULL DEFAULT true,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX habits_user_idx ON habits (user_id) WHERE active;

-- One completion per habit per local day. Same per-day idempotency idiom as
-- check_ins' UNIQUE (user_id, local_date), applied per habit: ticking twice
-- is the same as ticking once.
CREATE TABLE habit_completions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    habit_id     uuid        NOT NULL REFERENCES habits (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    local_date   date        NOT NULL,

    completed_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (habit_id, local_date)
);

CREATE INDEX habit_completions_user_date_idx ON habit_completions (user_id, local_date DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE habit_completions;
DROP TABLE habits;
-- +goose StatementEnd
