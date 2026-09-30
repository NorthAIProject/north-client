-- +goose Up
-- +goose StatementBegin

-- The first social layer: a public handle, invite links, follows that need the
-- other person's yes, and blocks. Nothing here exposes what somebody logged;
-- what a follower may see is decided later, category by category.

-- A handle is how somebody is found and linked to (/u/<handle>). Optional:
-- an account that never sets one cannot be looked up at all. Stored lower
-- case; citext makes the uniqueness case-blind either way.
ALTER TABLE users
    ADD COLUMN handle citext UNIQUE
        CHECK (handle::text ~ '^[a-z0-9_]{3,20}$');

-- One reusable code per person per channel, so a link posted once keeps
-- working for everybody who taps it, and the channel says which posting
-- brought them.
CREATE TABLE invites (
    code        text PRIMARY KEY CHECK (code ~ '^[a-z0-9]{10}$'),
    inviter_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel     text NOT NULL DEFAULT 'link'
        CHECK (channel IN ('link', 'messages', 'x', 'facebook', 'contacts')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (inviter_id, channel)
);

-- Who came in through which code. One row per new account at most: an
-- account is invited once, by the first link it redeemed.
CREATE TABLE invite_redemptions (
    invitee_id   uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code         text NOT NULL REFERENCES invites(code) ON DELETE CASCADE,
    redeemed_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invite_redemptions_code ON invite_redemptions (code);

-- A follow starts pending and becomes accepted when the other person says
-- yes. Growth data is personal, so nobody is followable without consent.
CREATE TABLE follows (
    follower_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followee_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    accepted_at  timestamptz,
    PRIMARY KEY (follower_id, followee_id),
    CHECK (follower_id <> followee_id)
);
CREATE INDEX follows_followee ON follows (followee_id, status);

CREATE TABLE blocks (
    blocker_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);
CREATE INDEX blocks_blocked ON blocks (blocked_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE blocks;
DROP TABLE follows;
DROP TABLE invite_redemptions;
DROP TABLE invites;
ALTER TABLE users DROP COLUMN handle;
-- +goose StatementEnd
