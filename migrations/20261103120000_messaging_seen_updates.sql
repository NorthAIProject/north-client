-- +goose Up
-- +goose StatementBegin

-- The platform updates already answered, one row each.
--
-- This replaces the last_update_id watermark as the redelivery check. A
-- watermark assumes updates are handled in the order they were sent, and they
-- are not: each one is answered on its own goroutine, and a photo is
-- downloaded before it is claimed. A text sent right after a photo therefore
-- claimed first, moved the watermark past the photo, and the photo was then
-- discarded as a "redelivery" with no reply and an INFO log line. A photo
-- album lost every picture that lost that race.
--
-- A set answers the real question, "has this exact update been handled?",
-- whatever order things arrive in. The primary key makes the insert the claim,
-- so two deliveries of one update still cannot both pass.
--
-- Keyed by account because an update id is a counter per bot (see
-- 20260829020000_messaging_account_scope.sql), not per chat. Rows are only
-- needed for as long as a platform might redeliver: Telegram keeps an
-- undelivered update for 24 hours, and the worker purges anything older than
-- two days.
CREATE TABLE messaging_seen_updates (
    platform   text        NOT NULL,
    account_id text        NOT NULL,
    update_id  bigint      NOT NULL,
    seen_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, account_id, update_id)
);

CREATE INDEX messaging_seen_updates_seen_at_idx ON messaging_seen_updates (seen_at);

-- Every update the watermark already accepted counts as seen, or the first
-- redelivery after this deploy would be answered a second time. Only the
-- latest per link is known, and it is the only one a platform is still likely
-- to resend.
INSERT INTO messaging_seen_updates (platform, account_id, update_id)
SELECT platform, account_id, last_update_id
FROM messaging_links
WHERE last_update_id > 0
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS messaging_seen_updates;
-- +goose StatementEnd
