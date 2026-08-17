-- +goose Up
-- +goose StatementBegin

-- An external MCP server one person has connected North to.
--
-- The outbound direction. agent_connections (20260813180000) is the inbound
-- one: tokens North issues so Claude Code or Codex can call in. These are
-- credentials North uses to call out, and conflating the two would mean one
-- revocation screen doing two opposite things.
--
-- One row per person per provider, so "connect my calendar" replaces rather
-- than accumulates.
CREATE TABLE integration_connections (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- What this connection is for: 'calendar' today. Not a CHECK constraint,
    -- for the reason the reports table has none on kind — adding a second
    -- provider should not need a migration.
    provider        text        NOT NULL,

    -- The MCP server's streamable HTTP endpoint.
    endpoint        text        NOT NULL,

    -- The bearer token, sealed with internal/shared/secret. Never stored in
    -- plaintext and never logged: unlike Strava's columns there is no
    -- unencrypted variant here, because this table is new and has no rows to
    -- migrate.
    token_sealed    bytea,

    -- Result of the last attempt to reach the server, for the settings page.
    -- 'ok' | 'failed'. last_error is a short human-readable summary, never the
    -- provider's response body, which can echo the token back.
    status          text        NOT NULL DEFAULT 'ok'
                                CHECK (status IN ('ok', 'failed')),
    last_error      text        NOT NULL DEFAULT '',
    last_checked_at timestamptz,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, provider)
);

CREATE INDEX integration_connections_user_idx
    ON integration_connections (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE integration_connections;
-- +goose StatementEnd
