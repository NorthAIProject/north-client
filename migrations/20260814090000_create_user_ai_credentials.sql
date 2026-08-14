-- +goose Up
-- +goose StatementBegin

-- The AI provider credential a user brought themselves.
--
-- One row per user, not one per provider. The product question is "who serves
-- my coach", and it has exactly one answer at a time; a second row would need
-- a second concept — which of them is active — bought at the price of a
-- composite key, for nothing.
CREATE TABLE user_ai_credentials (
    user_id       uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,

    -- Narrower than internal/config's knownProviders, and deliberately not
    -- CHECK-constrained against a literal list here: the valid set is the
    -- catalogue in internal/ai/providers, and repeating it in SQL would make
    -- two places to edit and one of them to forget. The service validates.
    provider      text        NOT NULL,

    -- AES-256-GCM, sealed by internal/shared/secret with the user id as
    -- additional data — so a row copied to another user fails to open rather
    -- than quietly serving one person from another's account. Never logged,
    -- never rendered, never selected into anything a template can reach.
    --
    -- The length floor catches a truncated blob: a sealed value is at least
    -- version + key id + nonce + tag. It cannot catch a plaintext key, and
    -- nothing at this layer could — that guarantee lives in the repository,
    -- whose signature takes sealed bytes and will not accept a string.
    api_key       bytea       NOT NULL CHECK (length(api_key) BETWEEN 30 AND 8192),

    -- The last four characters of the plaintext key, so the settings page can
    -- prove which key is stored without being able to show it.
    key_hint      text        NOT NULL DEFAULT '' CHECK (length(key_hint) <= 8),

    -- Empty means the catalogue's default for this provider. It cannot mean
    -- "let the client decide": openaicompat.New refuses an empty model.
    model         text        NOT NULL DEFAULT '' CHECK (length(model) <= 200),

    -- Why the last attempt failed, if it did. Written best-effort so the
    -- settings page can say "your key was rejected" rather than letting
    -- somebody discover it as a coach that quietly got worse. Never contains
    -- the key: the service writes a summary, not the provider's response.
    last_error    text        NOT NULL DEFAULT '' CHECK (length(last_error) <= 500),
    last_error_at timestamptz,

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- No secondary index: every read is by primary key.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_ai_credentials;
-- +goose StatementEnd
