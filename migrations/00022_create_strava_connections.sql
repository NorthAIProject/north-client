-- +goose Up
-- +goose StatementBegin

-- One Strava account per North account. Separate from auth_identities
-- because this is not a way to sign in: it is a data source that happens to
-- use OAuth, and conflating the two would mean disconnecting Strava could
-- lock someone out of their account.
CREATE TABLE strava_connections (
    user_id        uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,

    athlete_id     bigint      NOT NULL,

    -- Tokens are secrets. They are never logged, never rendered, and never
    -- leave the service layer. Stored in plaintext for now, matching how
    -- auth_identities holds provider data; encrypting at rest is a
    -- deliberate follow-up rather than something this migration pretends to
    -- have solved.
    access_token   text        NOT NULL,
    refresh_token  text        NOT NULL,
    expires_at     timestamptz NOT NULL,

    scopes         text        NOT NULL DEFAULT '',

    -- Where the next sync starts from. NULL means "never synced", which is
    -- what makes the first sync fetch a bounded backfill instead of
    -- everything the athlete has ever recorded.
    last_synced_at timestamptz,

    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE strava_connections;
-- +goose StatementEnd
