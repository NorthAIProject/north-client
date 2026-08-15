-- +goose Up
-- +goose StatementBegin

-- In-app coach nudges. Produced by the worker from deterministic rules
-- (missed check-in, approaching deadline), never by the model. Dismiss does
-- not delete: the unique key is the spam window, and a later sweep must not
-- recreate a nudge the person already put away.

CREATE TABLE user_nudges (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         text        NOT NULL
                             CHECK (kind IN ('missed_checkin', 'goal_deadline')),
    dedupe_key   text        NOT NULL,
    title        text        NOT NULL,
    body         text        NOT NULL,
    href         text        NOT NULL DEFAULT '',
    read_at      timestamptz,
    dismissed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX user_nudges_dedupe_idx
    ON user_nudges (user_id, kind, dedupe_key);

CREATE INDEX user_nudges_user_open_idx
    ON user_nudges (user_id, created_at DESC)
    WHERE dismissed_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_nudges;
-- +goose StatementEnd
