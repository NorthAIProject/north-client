-- +goose Up
-- +goose StatementBegin

-- The coach's short note under a saved check-in. One per check-in, replaced
-- when the check-in's content changes; content_hash is what it was written
-- for, so reopening the page or the app costs no second model call.
CREATE TABLE check_in_replies (
    check_in_id  uuid        PRIMARY KEY REFERENCES check_ins(id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content_hash text        NOT NULL,
    -- daily, or pattern on the check-ins where a pattern is named.
    kind         text        NOT NULL,
    text         text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX check_in_replies_user_idx ON check_in_replies (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE check_in_replies;
-- +goose StatementEnd
