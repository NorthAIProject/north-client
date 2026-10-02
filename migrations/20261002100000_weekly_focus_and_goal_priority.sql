-- +goose Up
-- +goose StatementBegin

-- The closed weekly loop: what someone chose to focus on for a week, written
-- when they finish the Sunday review. week_start is the Monday of the week the
-- focus is for, in their own time zone.
CREATE TABLE weekly_focus (
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start   date NOT NULL,
    priorities   text[] NOT NULL DEFAULT '{}' CHECK (cardinality(priorities) <= 3),
    -- How hard to train that week: hold the plan, build on it, or deload.
    volume       text NOT NULL DEFAULT 'hold' CHECK (volume IN ('hold', 'build', 'deload')),
    reviewed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, week_start)
);

-- The order someone put their active goals in, lowest first. NULL is
-- "not ranked", which sorts after every ranked goal.
ALTER TABLE goals ADD COLUMN priority int;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE goals DROP COLUMN priority;
DROP TABLE weekly_focus;
-- +goose StatementEnd
