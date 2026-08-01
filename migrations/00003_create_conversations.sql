-- +goose Up
-- +goose StatementBegin

CREATE TABLE conversations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Generated from the opening message. Null until then, so the UI can show
    -- a placeholder rather than an empty string.
    title      text,

    created_at timestamptz NOT NULL DEFAULT now(),

    -- Ordering key for the conversation list. Bumped when a message is
    -- appended, so "most recent" means most recently active rather than most
    -- recently created.
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX conversations_user_updated_idx ON conversations (user_id, updated_at DESC);

CREATE TABLE messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,

    -- 'user' or 'model', matching ai.Role. Constrained here so a typo in Go
    -- becomes a failed insert rather than a message that never renders.
    role            text        NOT NULL CHECK (role IN ('user', 'model')),

    content         text        NOT NULL,

    -- Attachments and any future non-text parts. Kept out of content so the
    -- text column stays directly searchable.
    parts           jsonb       NOT NULL DEFAULT '[]'::jsonb,

    -- Token counts and the model that produced this reply. Needed to explain a
    -- bill and to notice a context window filling up.
    usage           jsonb,
    model           text,
    provider        text,

    created_at      timestamptz NOT NULL DEFAULT now()
);

-- Every read of a conversation is "its messages, oldest first".
CREATE INDEX messages_conversation_created_idx ON messages (conversation_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE messages;
DROP TABLE conversations;
-- +goose StatementEnd
