-- +goose Up
-- +goose StatementBegin

CREATE TABLE sessions (
    -- SHA-256 of the token, never the token itself. A leaked database dump then
    -- does not hand out live sessions, the same reason password_hash exists.
    token_hash   bytea       PRIMARY KEY,

    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),

    -- Shown on a future "active sessions" screen so a user can recognise and
    -- revoke a login they do not remember.
    user_agent   text,
    ip           inet
);

-- Revoking every session for a user (password change, account compromise) has
-- to be a fast, common operation.
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Supports the periodic sweep of expired rows.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sessions;
-- +goose StatementEnd
