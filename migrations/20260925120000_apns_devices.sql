-- +goose Up
-- +goose StatementBegin

-- An iPhone that agreed to receive nudges from the app.
--
-- push_subscriptions holds browsers; a device token is a different thing with
-- a different lifecycle, so it gets its own table rather than a platform
-- column on that one. One row per token: a phone and an iPad are two rows and
-- either can go away on its own. The token is unique across users because
-- Apple issues it per app install; if that install signs in as somebody else,
-- the row follows the new account.
--
-- topic is the bundle identifier the token was issued for. The App Store app
-- and the TestFlight beta have different identifiers, and a send must name
-- the one the token belongs to or Apple refuses it. environment says which
-- host to send to: a build from Xcode gets a sandbox token, TestFlight and the
-- App Store get production ones, and a token sent to the wrong host is
-- refused as a bad token.
--
-- As with browsers, a token Apple declares gone is deleted, while any other
-- refusal stamps failed_at so an operator can tell a silent user from a
-- broken pipe.
CREATE TABLE apns_devices (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token        text        NOT NULL UNIQUE,
    topic        text        NOT NULL,
    environment  text        NOT NULL CHECK (environment IN ('production', 'sandbox')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    failed_at    timestamptz
);

CREATE INDEX apns_devices_user_idx ON apns_devices (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS apns_devices;
-- +goose StatementEnd
