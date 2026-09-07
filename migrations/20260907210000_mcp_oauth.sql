-- +goose Up
-- +goose StatementBegin

-- OAuth for the /mcp endpoint, so connecting an agent is one pasted URL
-- rather than a token copied into a configuration file.
--
-- Four new tables and five columns on agent_connections. The shape is the one
-- docs/mcp-oauth-plan.md argued for, with three departures recorded there:
-- issuance is its own column rather than a fifth client_kind, refresh tokens
-- are hashed rather than sealed, and one row is one *grant* rather than one
-- access token.

-- A client that registered itself under RFC 7591.
--
-- Registration is open, because requiring a gate would mean a human step in
-- the middle of the one-URL flow this exists to create. It is bounded instead:
-- rate limited per IP, capped in total, public clients only, and swept when a
-- registration never turns into a grant.
CREATE TABLE mcp_oauth_clients (
    -- "mcpc_" and 32 random bytes. Not a uuid: it is quoted back to the client
    -- as client_id and appears in logs, so a visible prefix is worth having.
    id            text        PRIMARY KEY,

    -- Chosen by the client at registration, and therefore attacker-controlled.
    -- It is rendered on the consent screen — templ escapes it — and the screen
    -- shows the redirect host beside it, because the host is the part a
    -- hostile registration cannot fake.
    client_name   text        NOT NULL,

    -- Matched exactly at both authorize and token. The one deviation is the
    -- RFC 8252 loopback exception, which lives in code rather than here
    -- because it compares scheme, host and path while ignoring the port.
    redirect_uris text[]      NOT NULL,
    grant_types   text[]      NOT NULL,

    software_id   text        NOT NULL DEFAULT '',

    -- sha256 of name, software_id, and the sorted normalised redirect URIs.
    -- A client that registers twice with identical metadata gets its existing
    -- row back instead of a new one. This dedupes a web client with a stable
    -- callback; it deliberately does not dedupe Claude Code, whose callback
    -- port is random, which is what the sweep is for.
    dedupe_key    bytea       NOT NULL UNIQUE,

    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);

-- An authorization request, parked server-side between the redirect in and the
-- Approve out.
--
-- The reason this table exists rather than the parameters riding in the query
-- string: the consent screen can create an account, so the request has to
-- survive a signup, a sign-in, or a round trip through Google or a passkey.
-- Carrying only an opaque id through those hops means none of the OAuth
-- parameters can be edited in between.
CREATE TABLE mcp_authorization_requests (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id             text        NOT NULL REFERENCES mcp_oauth_clients (id) ON DELETE CASCADE,
    redirect_uri          text        NOT NULL,
    state                 text        NOT NULL DEFAULT '',

    code_challenge        text        NOT NULL,
    code_challenge_method text        NOT NULL,

    scope                 text        NOT NULL,

    -- RFC 8707. Stored so the token request can be checked against what the
    -- authorize request asked for, rather than only against configuration.
    resource              text        NOT NULL,

    -- sha256 of the north_oauth_req cookie. Without it, a guessed request id
    -- lets somebody have a victim approve a request they started.
    verifier_hash         bytea       NOT NULL,

    -- Whether the account was created on the consent screen itself. This is
    -- the number that says whether the connector acquires anybody or only
    -- convenienced people who had already signed up.
    account_created       boolean     NOT NULL DEFAULT false,

    created_at            timestamptz NOT NULL DEFAULT now(),
    expires_at            timestamptz NOT NULL,
    consumed_at           timestamptz
);

CREATE INDEX mcp_authorization_requests_expiry_idx
    ON mcp_authorization_requests (expires_at)
    WHERE consumed_at IS NULL;

-- A one-time authorization code.
--
-- consumed_at rather than a delete, because replay detection needs to know a
-- code existed and was already used: a second presentation revokes the whole
-- grant (RFC 6749 section 10.5), and connection_id is how it finds it.
CREATE TABLE mcp_authorization_codes (
    -- The code itself is never stored, the same way agent_connections and
    -- sessions do it.
    code_hash             bytea       PRIMARY KEY,

    client_id             text        NOT NULL REFERENCES mcp_oauth_clients (id) ON DELETE CASCADE,
    user_id               uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    redirect_uri          text        NOT NULL,

    code_challenge        text        NOT NULL,
    code_challenge_method text        NOT NULL,

    scope                 text        NOT NULL,
    resource              text        NOT NULL,

    created_at            timestamptz NOT NULL DEFAULT now(),
    expires_at            timestamptz NOT NULL,
    consumed_at           timestamptz,

    -- Set at exchange. Null until then, and the FK is nullable because the
    -- grant does not exist while the code is still outstanding.
    connection_id         uuid        REFERENCES agent_connections (id) ON DELETE SET NULL
);

CREATE INDEX mcp_authorization_codes_expiry_idx
    ON mcp_authorization_codes (expires_at)
    WHERE consumed_at IS NULL;

-- +goose StatementEnd

-- +goose StatementBegin

-- What an OAuth grant adds to a connection.
--
-- Defaults are what every hand-issued token already is, so there is nothing to
-- backfill and the existing queries keep working with one added predicate.
ALTER TABLE agent_connections
    -- Empty means full access, which is every token issued before scopes
    -- existed. OAuth grants carry 'north:read' or 'north:read_write'.
    ADD COLUMN scopes          text NOT NULL DEFAULT '',

    -- NULL means "does not expire", which is every hand-issued token. An
    -- OAuth access token expires in an hour and is rotated in place on
    -- refresh.
    ADD COLUMN expires_at      timestamptz,

    -- How the credential was issued, which is a different question from
    -- client_kind. client_kind is which client the setup was generated for and
    -- is presentation only; overloading it to mean issuance would cost the one
    -- thing it means, and its CHECK constraint does not admit a fifth value.
    ADD COLUMN issuance        text NOT NULL DEFAULT 'pat'
                    CHECK (issuance IN ('pat', 'oauth')),

    -- The RFC 8707 audience this token was issued for. Checked on every
    -- request: binding the audience at authorize and token only stops a client
    -- from asking wrongly, not from replaying a token somewhere else.
    ADD COLUMN resource        text NOT NULL DEFAULT '',

    ADD COLUMN oauth_client_id text REFERENCES mcp_oauth_clients (id) ON DELETE SET NULL;

-- Access tokens expire, so the sweep needs to find them without scanning the
-- table. Hand-issued tokens have no expiry and are excluded.
CREATE INDEX agent_connections_expiring_idx
    ON agent_connections (expires_at)
    WHERE revoked_at IS NULL AND expires_at IS NOT NULL;

-- +goose StatementEnd

-- +goose StatementBegin

-- A refresh token, one row per issuance.
--
-- Hashed rather than sealed with internal/shared/secret. That package exists
-- so North can *use* a credential later — a BYOK provider key has to be
-- decrypted and sent upstream. A token North issued is only ever verified, so
-- a hash is strictly better: a database dump cannot be replayed and there is
-- no key to rotate. agent_connections.token_hash made the same call.
--
-- Single use. Refreshing consumes the row and writes a new one, and presenting
-- a consumed token revokes the grant.
CREATE TABLE mcp_refresh_tokens (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id uuid        NOT NULL REFERENCES agent_connections (id) ON DELETE CASCADE,
    token_hash    bytea       NOT NULL UNIQUE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    consumed_at   timestamptz,
    replaced_by   uuid        REFERENCES mcp_refresh_tokens (id) ON DELETE SET NULL
);

-- The live token for a connection is the unconsumed one, which is the lookup
-- every refresh performs.
CREATE INDEX mcp_refresh_tokens_connection_idx
    ON mcp_refresh_tokens (connection_id)
    WHERE consumed_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Order matters. mcp_authorization_codes and mcp_refresh_tokens reference
-- agent_connections, and agent_connections references mcp_oauth_clients, so
-- the column has to go before the table it points at.
DROP TABLE IF EXISTS mcp_refresh_tokens;
DROP TABLE IF EXISTS mcp_authorization_codes;
DROP TABLE IF EXISTS mcp_authorization_requests;

DROP INDEX IF EXISTS agent_connections_expiring_idx;

ALTER TABLE agent_connections
    DROP COLUMN IF EXISTS oauth_client_id,
    DROP COLUMN IF EXISTS resource,
    DROP COLUMN IF EXISTS issuance,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS scopes;

DROP TABLE IF EXISTS mcp_oauth_clients;

-- +goose StatementEnd
