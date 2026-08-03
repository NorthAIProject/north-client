-- +goose Up
-- +goose StatementBegin

CREATE TABLE goals (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    title       text        NOT NULL,

    -- Why this matters to them. Fed to the coach, because a goal without a
    -- reason is a task, and North is meant to help with the difference.
    motivation  text        NOT NULL DEFAULT '',

    -- How they will know it happened. Deliberately free text rather than a
    -- number: "run 10k without stopping" and "sleep before midnight most
    -- nights" are both real goals and neither is a metric.
    success     text        NOT NULL DEFAULT '',

    category    text        NOT NULL DEFAULT 'other',

    status      text        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'achieved', 'abandoned', 'paused')),

    -- Null means no deadline, which is a legitimate and common answer.
    target_date date,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- When it left 'active'. Kept so a finished goal still shows when it was
    -- finished, which is most of the satisfaction.
    closed_at   timestamptz
);

-- The coach loads active goals on every message, so this is the hot path.
CREATE INDEX goals_user_status_idx ON goals (user_id, status, created_at DESC);

CREATE TABLE goal_updates (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_id    uuid        NOT NULL REFERENCES goals (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    note       text        NOT NULL,

    -- Optional self-assessment, 0-100. Null when the note is just a note.
    progress   smallint    CHECK (progress IS NULL OR progress BETWEEN 0 AND 100),

    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX goal_updates_goal_created_idx ON goal_updates (goal_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE goal_updates;
DROP TABLE goals;
-- +goose StatementEnd
