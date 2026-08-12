-- +goose Up
-- +goose StatementBegin

-- Checkpoints on the way to a goal. Progress notes (goal_updates) are a
-- journal; these are the structure. Completing the last one does not close
-- the goal — "done when" is still the person's words.
CREATE TABLE goal_milestones (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_id      uuid        NOT NULL REFERENCES goals (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    title        text        NOT NULL,

    status       text        NOT NULL DEFAULT 'open'
                             CHECK (status IN ('open', 'completed')),

    -- Append-only ordering. New rows take MAX(position)+1 for the goal.
    position     int         NOT NULL DEFAULT 0,

    -- Optional, same meaning as goals.target_date: a checkpoint with no date
    -- is common and fine. A date in the past is allowed — a missed checkpoint
    -- is a real thing, unlike a goal created already overdue.
    target_date  date,

    -- Stamped when status becomes 'completed' and cleared if it comes back,
    -- matching goals.closed_at.
    completed_at timestamptz,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX goal_milestones_goal_idx
    ON goal_milestones (goal_id, position, created_at);

-- Isolation path: every write is scoped by user_id without joining goals.
CREATE INDEX goal_milestones_user_idx
    ON goal_milestones (user_id, goal_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE goal_milestones;
-- +goose StatementEnd
