-- +goose Up
-- +goose StatementBegin

-- The record of a person taking their data out, or asking to be gone.
--
-- Every other account of what North did lives in a table that cascades with the
-- user — tool_executions says so in as many words. That is right for a log of
-- what was done *to* someone's data on their behalf. It is useless for the one
-- event that happens as the account itself goes: a delete row in a cascading
-- table deletes itself in the same statement that writes it.
--
-- So this table has no foreign key, and that is the entire point of it. What it
-- keeps is deliberately thin — an id, a moment, and what happened — because a
-- record that outlives its subject has to justify every column it holds after
-- the person asked to be forgotten. The email is not here. Nothing here
-- reconstructs anybody.
CREATE TABLE account_events (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Deliberately not a foreign key. The row's whole value is outliving the
    -- account it describes, and after a delete this points at nothing — which
    -- is the correct end state, not a dangling reference to be cleaned up.
    user_id    uuid        NOT NULL,

    -- 'export' or 'delete'. Unconstrained for the same reason
    -- tool_executions.surface is: a third kind of account event should need a
    -- line of Go, not a migration.
    event      text        NOT NULL,

    -- What the event is worth knowing afterwards. For a delete: how many stored
    -- objects were removed and how many could not be, which is the only way to
    -- find out later that a bucket kept bytes it was asked to drop.
    detail     jsonb,

    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX account_events_user_created_idx
    ON account_events (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE account_events;
-- +goose StatementEnd
