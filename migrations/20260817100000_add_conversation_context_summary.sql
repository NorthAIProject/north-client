-- +goose Up
-- +goose StatementBegin

-- The rolling compaction of a long thread's older turns.
--
-- Deliberately NOT the existing conversations.summary column. That one is the
-- closing write-up of a reflection, and Conversation.Ended() is true precisely
-- when it is non-empty — so writing a chat thread's compaction into it would
-- mark reflections finished that had not finished, and would put a summary on
-- chat threads that are documented as never having one.
ALTER TABLE conversations
    ADD COLUMN context_summary text NOT NULL DEFAULT '';

-- The newest message already folded into context_summary.
--
-- A watermark rather than a message count so each pass is incremental: the
-- summariser reads only the turns written since it last ran, and folds them
-- into what it already wrote. NULL means nothing has been summarised yet.
--
-- created_at rather than a message id because that is what the summariser
-- orders by, and messages_conversation_created_idx already covers it.
ALTER TABLE conversations
    ADD COLUMN context_summary_through timestamptz;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE conversations
    DROP COLUMN context_summary_through;

ALTER TABLE conversations
    DROP COLUMN context_summary;
-- +goose StatementEnd
