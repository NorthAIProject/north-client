-- +goose Up
-- +goose StatementBegin

-- One row per messaging account a user has linked to their own account.
--
-- The shape is auth_identities' (00008): UNIQUE (platform, external_id) is the
-- inbound join key, because a message arrives carrying a chat id and nothing
-- else, and the only question worth asking is whose account it belongs to.
--
-- It is a separate table from auth_identities for the reason 00022 gives for
-- strava_connections: this is not a way to sign in. Conflating the two would
-- mean unlinking Telegram could lock somebody out of their account, which is a
-- steep price for a convenience.
CREATE TABLE messaging_links (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Deliberately unconstrained, following the reasoning in 20260815170000:
    -- a CHECK whose only effect is to require a schema change per platform is
    -- not protecting the data. The strings callers may use are bounded at the
    -- edge, where the adapters are.
    platform       text        NOT NULL,

    -- The platform's own stable id for the conversation this person messages
    -- from — a Telegram chat id. Text rather than bigint because the next
    -- platform's is a string and this column should not need a migration to
    -- find that out.
    external_id    text        NOT NULL,

    -- High-water mark for delivery ids, monotonic per bot.
    --
    -- Platforms redeliver an update whose acknowledgement was lost, and this
    -- adapter acknowledges before it answers. Without a watermark a dropped
    -- 200 would coach the same sentence twice and, worse, could run a
    -- confirmed write twice.
    last_update_id bigint      NOT NULL DEFAULT 0,

    created_at     timestamptz NOT NULL DEFAULT now(),

    -- NULL means "linked but never used", which is how the settings page tells
    -- a live connection from one set up and forgotten. Same role as
    -- agent_connections.last_used_at.
    last_seen_at   timestamptz,

    UNIQUE (platform, external_id)
);

-- Listings are always "the platforms this one user has linked".
CREATE INDEX messaging_links_user_id_idx ON messaging_links (user_id);

-- A short-lived, single-use code that proves a chat belongs to an account.
--
-- The flow it exists for: the web app, where the person is already
-- authenticated, issues a code; they send it to the bot; the bot's adapter
-- redeems it. That is what carries the identity across, and it is the only
-- moment an unlinked chat is allowed to reach anything.
CREATE TABLE messaging_link_codes (
    -- The code itself is never stored, only its SHA-256 hash — the same way
    -- sessions (00002), password resets (00007) and agent connections
    -- (20260813180000) do it. A database dump therefore cannot be redeemed.
    --
    -- The hash is the primary key because redemption looks a code up by
    -- exactly one thing, and a code that hashes to an existing row is either
    -- the same code or an event too rare to design for.
    code_hash   bytea       PRIMARY KEY,

    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform    text        NOT NULL,

    -- Short by design. The code is typed by hand into a chat window, so it is
    -- deliberately weaker than a bearer token; the expiry, single use, and a
    -- rate limit on redemption are what make that safe rather than the length.
    expires_at  timestamptz NOT NULL,

    -- A tombstone rather than a delete, so a second attempt with a spent code
    -- can be told apart from one that never existed. Both are refused; only
    -- the logs need to know the difference.
    redeemed_at timestamptz,

    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Supports "does this person already have a code outstanding?" on the settings
-- page, and the sweep that clears expired ones.
CREATE INDEX messaging_link_codes_user_id_idx ON messaging_link_codes (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE messaging_link_codes;
DROP TABLE messaging_links;
-- +goose StatementEnd
