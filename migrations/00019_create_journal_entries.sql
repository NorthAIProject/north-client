-- +goose Up
-- +goose StatementBegin

CREATE TABLE journal_entries (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    content    text        NOT NULL,
    -- Optional, mirrors check-ins' 1-5 scale — a reflection doesn't have to
    -- come with a mood rating to be worth writing down.
    mood       smallint    CHECK (mood IS NULL OR mood BETWEEN 1 AND 5),

    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX journal_entries_user_created_idx ON journal_entries (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE journal_entries;
-- +goose StatementEnd
