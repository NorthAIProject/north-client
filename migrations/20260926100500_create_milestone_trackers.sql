-- +goose Up
-- +goose StatementBegin

-- "Months since": the dentist, a haircut, changing the toothbrush. Not a habit
-- — nothing is scheduled and nothing is missed — but a standing thing whose
-- age is worth seeing. One row per tracker, its date moved forward when done.
CREATE TABLE milestone_trackers (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    name            text        NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    last_done_on    date        NOT NULL,
    -- How often it is due, when that is known. Drives the gauge's fill.
    interval_months smallint    CHECK (interval_months BETWEEN 1 AND 60),

    created_at      timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, name)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE milestone_trackers;
-- +goose StatementEnd
