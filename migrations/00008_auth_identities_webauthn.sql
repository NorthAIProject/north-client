-- +goose Up
-- +goose StatementBegin

-- OAuth and passkey-only accounts have no password.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

-- External identity providers (Google today). One row per linked provider
-- account; UNIQUE(provider, provider_subject) is the stable join key from the
-- IdP, while email is informational and may change on the provider side.
CREATE TABLE auth_identities (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider         text        NOT NULL,
    provider_subject text        NOT NULL,
    email            text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_subject)
);

CREATE INDEX auth_identities_user_id_idx ON auth_identities (user_id);

-- Stored public keys for WebAuthn / passkeys. credential_id is the authenticator
-- handle returned by the browser and is the natural primary key.
CREATE TABLE webauthn_credentials (
    credential_id    bytea PRIMARY KEY,
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    public_key       bytea       NOT NULL,
    attestation_type text        NOT NULL DEFAULT '',
    transport        text[]      NOT NULL DEFAULT '{}',
    sign_count       bigint      NOT NULL DEFAULT 0,
    name             text        NOT NULL DEFAULT '',
    aaguid           bytea,
    backup_eligible  boolean     NOT NULL DEFAULT false,
    backup_state     boolean     NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_used_at     timestamptz
);

CREATE INDEX webauthn_credentials_user_id_idx ON webauthn_credentials (user_id);

-- Short-lived ceremony state for begin/finish registration and login.
-- The browser never holds this; it only gets the opaque challenge id.
CREATE TABLE webauthn_challenges (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    data       bytea       NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX webauthn_challenges_expires_at_idx ON webauthn_challenges (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS webauthn_challenges;
DROP TABLE IF EXISTS webauthn_credentials;
DROP TABLE IF EXISTS auth_identities;

-- Re-adding NOT NULL fails if any null hashes exist; clear them first is not
-- done here — down migrations are for local rollback, not production recovery.
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;

-- +goose StatementEnd
