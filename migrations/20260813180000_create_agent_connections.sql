-- +goose Up
-- +goose StatementBegin

-- One row per agent a user has connected to their own account.
--
-- This is what replaces the single MCP_API_TOKEN/MCP_USER_ID pair for the
-- publicly mounted /mcp endpoint. That pair maps one static bearer to one
-- hardcoded account, which is why it belongs on a tailnet; these rows carry
-- the identity instead, so the endpoint can serve every user without any of
-- them being able to act as another.
CREATE TABLE agent_connections (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- The user's own label for the machine or agent this token lives on
    -- ("laptop", "work desktop"). Shown on the settings page so revoking the
    -- right one does not require guessing.
    name         text        NOT NULL,

    -- Which client the setup instructions were generated for. Presentation
    -- only: nothing about authentication varies by kind, and a token issued
    -- for one client works in any of them.
    client_kind  text        NOT NULL DEFAULT 'other'
                 CHECK (client_kind IN ('claude_code', 'codex', 'hermes', 'other')),

    -- The token itself is never stored. Only its SHA-256 hash is, the same
    -- way sessions (00002) and password resets (00007) do it, so a database
    -- dump cannot be replayed against /mcp.
    token_hash   bytea       NOT NULL UNIQUE,

    -- The first few characters, kept in clear so the settings page can tell
    -- two connections apart without holding either token. Not a secret: it is
    -- far too short to guess the rest from.
    token_prefix text        NOT NULL,

    created_at   timestamptz NOT NULL DEFAULT now(),

    -- NULL means "never used", which is how the settings page distinguishes a
    -- connection that was set up from one that was issued and forgotten.
    -- Written lazily rather than on every request; see TouchAgentConnection.
    last_used_at timestamptz,

    -- Revocation is a tombstone, not a delete: the row is what tells the user
    -- a token they no longer recognise has been turned off, and last_used_at
    -- is the evidence for whether it mattered.
    revoked_at   timestamptz
);

-- Every listing is "the live connections for one user", so the index carries
-- that predicate rather than making the planner filter revoked rows out.
CREATE INDEX agent_connections_user_id_idx
    ON agent_connections (user_id)
    WHERE revoked_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE agent_connections;
-- +goose StatementEnd
