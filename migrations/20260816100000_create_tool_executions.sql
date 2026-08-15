-- +goose Up
-- +goose StatementBegin

-- What North has actually done to somebody's data on their behalf.
--
-- The coach's calls already survive in messages.tool_calls, but that only
-- answers for the coach: an external MCP client calls the same capabilities and
-- never touches a conversation, so its writes — the ones with no confirmation
-- step in front of them — would be the least visible of all. This is the one
-- place both surfaces are answerable from.
CREATE TABLE tool_executions (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Cascades with the account. Keeping an audit trail after the person it
    -- describes has asked to be forgotten would be the wrong kind of thorough.
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    tool       text        NOT NULL,

    -- Stored as they were sent. An audit line without them — "logged a
    -- check-in" — does not answer the question this table exists for, which is
    -- what was written, not merely that something was.
    arguments  jsonb,

    -- 'coach' or 'mcp'. Unconstrained for the same reason health_metrics.source
    -- is: a third surface should need a line of Go, not a migration.
    surface    text        NOT NULL,

    -- 'executed', 'failed', or 'declined'. Declined earns its own row because a
    -- refusal is a decision worth being able to point at later, and it never
    -- reaches the code path the other two do.
    outcome    text        NOT NULL,

    -- What the tool said, or why it failed. Free text: it is read by a person,
    -- not matched on.
    detail     text,

    created_at timestamptz NOT NULL DEFAULT now()
);

-- Serves the only read there is: one person's executions, newest first.
CREATE INDEX tool_executions_user_created_idx
    ON tool_executions (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE tool_executions;
-- +goose StatementEnd
