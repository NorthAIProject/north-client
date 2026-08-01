-- +goose Up
-- +goose StatementBegin

-- citext gives case-insensitive email comparison in the database rather than
-- relying on every call site remembering to lowercase first.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         citext      NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    display_name  text        NOT NULL,

    -- North coaches around the user's day, so a briefing sent at "07:00" has to
    -- mean their 07:00. Stored from signup rather than guessed per request.
    timezone      text        NOT NULL DEFAULT 'UTC',

    -- Free text describing how this user wants to be coached. It is fed to the
    -- prompt builder, so it is deliberately not an enum: the vocabulary here
    -- should be the user's, not a schema author's.
    coaching_style text,

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE users;
-- +goose StatementEnd
