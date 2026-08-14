-- +goose Up
-- +goose StatementBegin

-- When memory extraction last ran for this thread.
--
-- Records that the thread was *looked at*, not that anything was found. A
-- conversation that yielded no durable facts is the common case, and without
-- this column the idle sweep would re-run an AI extraction over it on every
-- pass, forever, at a cost proportional to how uneventful the conversation was.
--
-- Null means never extracted. A value older than updated_at means new messages
-- have arrived since, so the thread is worth another look.
ALTER TABLE conversations
    ADD COLUMN memories_extracted_at timestamptz;

-- The sweep's predicate: quiet threads, oldest activity first.
CREATE INDEX conversations_extraction_sweep_idx
    ON conversations (updated_at)
    WHERE memories_extracted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS conversations_extraction_sweep_idx;
ALTER TABLE conversations DROP COLUMN memories_extracted_at;
-- +goose StatementEnd
