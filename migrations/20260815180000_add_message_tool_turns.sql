-- +goose Up
-- +goose StatementBegin

-- The tool calls a model made on a turn, and the results handed back to it.
--
-- Until now these lived only in memory, for the length of one SSE stream: the
-- loop appended them to the request it was building and nothing was stored. So
-- a conversation could not be rebuilt mid-turn, which is exactly what pausing
-- to ask a person for approval requires — the stream ends, and the request has
-- to be reconstructed from the database when they answer.
--
-- Not a new role. ai.Message already carries tool calls and results as fields
-- alongside 'user' and 'model' (a call is a model turn, a result is a user
-- turn), so the CHECK constraint on role is still right and is left alone.
ALTER TABLE messages
    ADD COLUMN tool_calls   jsonb,
    ADD COLUMN tool_results jsonb;

-- Nullable rather than defaulting to '[]': the overwhelming majority of
-- messages are ordinary text and carry neither. A null says "this turn had
-- nothing to do with tools", which is a different claim from "it called no
-- tools", and it keeps the common row narrow.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE messages
    DROP COLUMN tool_calls,
    DROP COLUMN tool_results;
-- +goose StatementEnd
