-- +goose Up
-- +goose StatementBegin

-- Pays the debt 00022 promised: "Stored in plaintext for now ... encrypting at
-- rest is a deliberate follow-up rather than something this migration pretends
-- to have solved." internal/shared/secret now exists, so it is payable.
--
-- Additive rather than a type change, for two reasons that both have to hold at
-- once:
--
--   1. Existing rows hold plaintext, and SQL has no key to seal them with. They
--      are read from the old columns until the next token refresh rewrites them
--      sealed — which Strava forces within six hours, so the plaintext drains
--      on its own without a backfill job.
--   2. Encryption is optional. A deployment with no ENCRYPTION_KEY keeps
--      writing plaintext, exactly as today, rather than losing the integration.
--
-- Dropping the text columns is a later migration, once nothing writes them.
ALTER TABLE strava_connections
    ADD COLUMN access_token_sealed  bytea,
    ADD COLUMN refresh_token_sealed bytea;

-- Defaulted so a sealed write does not have to supply the plaintext columns it
-- is deliberately leaving empty.
ALTER TABLE strava_connections
    ALTER COLUMN access_token  SET DEFAULT '',
    ALTER COLUMN refresh_token SET DEFAULT '';

-- Exactly one of each pair carries the token. Neither being present would be a
-- connection that cannot be refreshed, which is worth refusing at write time
-- rather than discovering at the next sync.
ALTER TABLE strava_connections
    ADD CONSTRAINT strava_connections_access_token_present
        CHECK (access_token <> '' OR access_token_sealed IS NOT NULL),
    ADD CONSTRAINT strava_connections_refresh_token_present
        CHECK (refresh_token <> '' OR refresh_token_sealed IS NOT NULL);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE strava_connections
    DROP CONSTRAINT strava_connections_access_token_present,
    DROP CONSTRAINT strava_connections_refresh_token_present;

ALTER TABLE strava_connections
    ALTER COLUMN access_token  DROP DEFAULT,
    ALTER COLUMN refresh_token DROP DEFAULT;

ALTER TABLE strava_connections
    DROP COLUMN access_token_sealed,
    DROP COLUMN refresh_token_sealed;
-- +goose StatementEnd
