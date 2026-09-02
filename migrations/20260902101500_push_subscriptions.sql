-- +goose Up
-- +goose StatementBegin

-- A browser that agreed to be tapped on the shoulder.
--
-- The nudge engine has decided for months what North should say and when:
-- a missed check-in, a goal due this week, a training day. Until now that
-- decision reached a person only if they were already inside the app (the
-- bell) or had linked a Telegram bot. Neither tests the thing a native app
-- would be built for — whether a note on the lock screen brings somebody
-- back. Web Push through the service worker that already ships is that test.
--
-- One row per browser subscription, not per user: a phone and a laptop are
-- two endpoints and either can go away on its own. The endpoint is unique
-- across all users rather than per user because a push service issues it per
-- browser profile; if that browser signs in as somebody else, the row should
-- follow the new account, and ON CONFLICT (endpoint) does exactly that.
--
-- p256dh and auth are the browser's public key and auth secret, both
-- base64url as PushSubscription.toJSON() emits them. They encrypt the payload
-- to this one browser and are useless to anybody without the matching private
-- key, which never leaves the device. The VAPID keys that sign the request
-- live in configuration, not here.
--
-- failed_at is the last time a send was refused for a reason other than the
-- subscription being gone. A 404 or 410 deletes the row outright — the push
-- service has said the browser will never hear from us again — while a 5xx
-- or a network error leaves it and stamps this, so an operator can tell a
-- silent user from a broken pipe. last_used_at is the last accepted send.
CREATE TABLE push_subscriptions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint     text        NOT NULL UNIQUE,
    p256dh       text        NOT NULL,
    auth         text        NOT NULL,
    user_agent   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    failed_at    timestamptz
);

CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS push_subscriptions;
-- +goose StatementEnd
