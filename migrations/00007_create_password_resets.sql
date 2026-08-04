-- +goose Up
-- +goose StatementBegin

-- One-time tokens for the forget-password journey. Same rule as sessions: the
-- database stores only the SHA-256 of the token, so a dump cannot be replayed
-- as a live reset link.
CREATE TABLE password_reset_tokens (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    used_at    timestamptz
);

-- Issuing a new reset invalidates prior unused tokens for that user.
CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens (user_id);

-- Supports the periodic sweep of expired / used rows.
CREATE INDEX password_reset_tokens_expires_at_idx ON password_reset_tokens (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE password_reset_tokens;
-- +goose StatementEnd
