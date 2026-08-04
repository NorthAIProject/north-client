-- +goose Up
-- +goose StatementBegin

-- Durable facts about a person. Pending rows are proposed (usually by
-- extraction) and never reach the coach until approved.
CREATE TABLE user_memories (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    category               text        NOT NULL DEFAULT 'general',
    content                text        NOT NULL,

    status                 text        NOT NULL DEFAULT 'pending'
                                       CHECK (status IN ('pending', 'approved', 'rejected')),
    pinned                 boolean     NOT NULL DEFAULT false,

    -- user = typed by the person; extraction = proposed from a conversation.
    source                 text        NOT NULL DEFAULT 'user'
                                       CHECK (source IN ('user', 'extraction')),
    source_conversation_id uuid        REFERENCES conversations (id) ON DELETE SET NULL,

    confidence             real        CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),

    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    deleted_at             timestamptz
);

CREATE INDEX user_memories_user_status_idx
    ON user_memories (user_id, status, pinned DESC, updated_at DESC)
    WHERE deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_memories;
-- +goose StatementEnd
