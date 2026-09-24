-- +goose Up
-- +goose StatementBegin

-- Opaque data a provider needs back with the tool calls it made, kept beside
-- them so a turn resumed after an approval can replay them. Anthropic refuses a
-- replayed tool call without the thinking blocks that led to it, unchanged and
-- in order; this is where they live between the pause and the resume.
--
-- Owned by the client that wrote it. Nothing else reads it, and it is null on
-- every row but a tool call from a provider that has state to keep.
ALTER TABLE messages ADD COLUMN provider_state jsonb;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE messages DROP COLUMN provider_state;
-- +goose StatementEnd
