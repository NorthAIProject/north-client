-- +goose Up
-- +goose StatementBegin

-- Two more ways for friends to find each other: a verified phone number that
-- contacts on a phone can match, and Facebook friends who connected Khepri.

-- A verified phone number, E.164. Only ever set once a texted code proved
-- possession, so the pair is all or nothing. Never shown to anybody else:
-- the number is matched by its hash and otherwise only read back to its
-- owner.
--
-- Unique: when a second account verifies a number another already holds, the
-- number moves to the newer one. They just proved they have the phone; the
-- older holder most likely changed numbers and the carrier recycled it.
ALTER TABLE users
    ADD COLUMN phone_e164 text UNIQUE
        CHECK (phone_e164 ~ '^\+[1-9][0-9]{7,14}$'),
    ADD COLUMN phone_verified_at timestamptz,
    ADD CONSTRAINT users_phone_verified CHECK ((phone_e164 IS NULL) = (phone_verified_at IS NULL));

-- One row per code texted. It is both the pending verification (the latest
-- open row, for ten minutes) and the rate limit: texts cost money, so starts
-- are counted per account per hour and per number per day. Rows older than
-- two days count for nothing and are pruned on the next start.
CREATE TABLE phone_verifications (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    phone_e164  text NOT NULL,
    -- Codes checked against this text, right or wrong.
    attempts    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- Verified, replaced by a newer text, or abandoned.
    closed_at   timestamptz
);
CREATE INDEX phone_verifications_user ON phone_verifications (user_id, created_at DESC);
CREATE INDEX phone_verifications_phone ON phone_verifications (phone_e164, created_at DESC);

-- Pending Facebook connections started from the iOS app, the same
-- arrangement as strava_oauth_states: the app's sign-in sheet has no session
-- cookie, so the state alone says whose connection it is. Hashed, single
-- use, ten minutes.
CREATE TABLE facebook_oauth_states (
    state_hash  bytea PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL
);

-- What the last Facebook import found, handed from the OAuth callback to the
-- page or the app that shows it. Khepri account ids only: no Facebook id, no
-- name, no token. It lives an hour; finding friends again means connecting
-- again, because the token that read the friend list is never kept.
CREATE TABLE facebook_imports (
    user_id      uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    people       uuid[] NOT NULL,
    imported_at  timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE facebook_imports;
DROP TABLE facebook_oauth_states;
DROP TABLE phone_verifications;
ALTER TABLE users
    DROP CONSTRAINT users_phone_verified,
    DROP COLUMN phone_verified_at,
    DROP COLUMN phone_e164;
-- +goose StatementEnd
