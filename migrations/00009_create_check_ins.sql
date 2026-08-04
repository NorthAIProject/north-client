-- +goose Up
-- +goose StatementBegin

-- One reflection per local calendar day. local_date is computed in the user's
-- timezone at write time, so "today" means their today, not UTC.
CREATE TABLE check_ins (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    local_date      date        NOT NULL,

    mood            smallint    NOT NULL CHECK (mood BETWEEN 1 AND 5),
    energy          smallint    NOT NULL CHECK (energy BETWEEN 1 AND 5),

    wins            text        NOT NULL DEFAULT '',
    challenges      text        NOT NULL DEFAULT '',
    notes           text        NOT NULL DEFAULT '',

    related_goal_id uuid        REFERENCES goals (id) ON DELETE SET NULL,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, local_date)
);

CREATE INDEX check_ins_user_date_idx ON check_ins (user_id, local_date DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE check_ins;
-- +goose StatementEnd
