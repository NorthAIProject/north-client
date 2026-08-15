-- +goose Up
-- +goose StatementBegin

-- Reflection is a conversation kind, not a second coach. Summary is empty
-- until the session closes; a non-empty value is what hides the composer.

ALTER TABLE conversations
    ADD COLUMN kind text NOT NULL DEFAULT 'chat',
    ADD COLUMN summary text NOT NULL DEFAULT '';

ALTER TABLE conversations
    ADD CONSTRAINT conversations_kind_check
    CHECK (kind IN ('chat', 'reflection'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_kind_check;
ALTER TABLE conversations DROP COLUMN IF EXISTS summary;
ALTER TABLE conversations DROP COLUMN IF EXISTS kind;
-- +goose StatementEnd
