-- +goose Up
-- +goose StatementBegin

-- The daily post: one short piece on wellness, food or wellbeing, written by
-- the model once a UTC day and read by everyone. Nobody authors it, so it has
-- no user_id; ai_generated is stored rather than implied so a later
-- hand-written post cannot be mistaken for the model's, or the reverse.
CREATE TABLE daily_posts (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The UTC day it is for. Unique: the hourly job and any second worker
    -- race to insert, and only one wins.
    post_date      date        NOT NULL UNIQUE,
    theme          text        NOT NULL,
    title          text        NOT NULL,
    body           text        NOT NULL,
    ai_generated   boolean     NOT NULL DEFAULT true,
    -- What wrote it and with which prompt, for when one reads badly.
    model          text        NOT NULL DEFAULT '',
    prompt_version text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- Kudos on a daily post. Apart from kudos, which belongs to achievements.
CREATE TABLE daily_post_kudos (
    post_id    uuid        NOT NULL REFERENCES daily_posts (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE daily_post_kudos;
DROP TABLE daily_posts;
-- +goose StatementEnd
