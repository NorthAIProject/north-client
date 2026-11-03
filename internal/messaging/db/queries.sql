-- ClaimMessagingUpdate resolves the sender and rejects a redelivery in one
-- statement.
--
-- These are the same question. An adapter acknowledges an update before it
-- answers it, so the platform will redeliver anything whose acknowledgement
-- was lost; doing the redelivery check separately from the lookup would leave
-- a window where two deliveries of one message both pass it. Inserting the
-- update into messaging_seen_updates is the claim: its primary key lets
-- exactly one delivery through, and a concurrent second one waits for the
-- first and then conflicts.
--
-- A set rather than a high-water mark because updates are not handled in the
-- order they were sent. Under a watermark, a text that claimed before the
-- photo sent ahead of it made the photo look like a redelivery, and it was
-- dropped. An id never seen before is new, whatever its size.
--
-- Updates are keyed by account because ids are a sequence per bot, so a
-- different bot's ids are a new sequence, not old ones running backwards.
-- last_update_id is still kept, as the highest id this link has answered, for
-- operators; nothing decides on it any more.
--
-- No rows means one of two things — not linked, or already seen — and the
-- caller distinguishes them with GetMessagingLink. Cheap, because that only
-- happens off the common path.
-- name: ClaimMessagingUpdate :one
WITH link AS (
    SELECT l.id FROM messaging_links l
    WHERE l.platform = sqlc.arg(platform) AND l.external_id = sqlc.arg(external_id)
),
seen AS (
    INSERT INTO messaging_seen_updates (platform, account_id, update_id)
    SELECT sqlc.arg(platform)::text, sqlc.arg(account_id)::text, sqlc.arg(update_id)::bigint
    FROM link
    ON CONFLICT DO NOTHING
    RETURNING update_id
)
UPDATE messaging_links ml
SET last_update_id = CASE
                         WHEN ml.account_id IS DISTINCT FROM sqlc.arg(account_id)
                             THEN seen.update_id
                         ELSE greatest(ml.last_update_id, seen.update_id)
                     END,
    account_id     = sqlc.arg(account_id),
    last_seen_at   = now()
FROM link, seen
WHERE ml.id = link.id
RETURNING ml.*;

-- name: PurgeSeenMessagingUpdates :execrows
-- Forgets updates too old for any platform to resend. Telegram keeps an
-- undelivered update for 24 hours, so two days leaves room for a slow sweep.
DELETE FROM messaging_seen_updates
WHERE seen_at < now() - make_interval(secs => sqlc.arg(older_than_seconds)::double precision);

-- name: GetMessagingLink :one
SELECT * FROM messaging_links
WHERE platform = $1 AND external_id = $2;

-- name: ListMessagingLinksByUser :many
SELECT * FROM messaging_links
WHERE user_id = $1
ORDER BY created_at;

-- InsertMessagingLink deliberately has no ON CONFLICT clause.
--
-- A chat that is already linked is not a race to paper over: linked to the
-- same account it is a no-op, and linked to a different one it is somebody
-- trying to attach a stranger's chat to their own account. Those need
-- different answers, so the unique violation is raised and the caller decides
-- — the same shape as linkGoogleIdentity in internal/auth.
-- name: InsertMessagingLink :one
INSERT INTO messaging_links (user_id, platform, external_id, last_seen_at)
VALUES ($1, $2, $3, now())
RETURNING *;

-- Scoped by user_id as well as platform: this is reached from a form, and
-- without the second predicate it would unlink by platform alone.
-- name: DeleteMessagingLink :execrows
DELETE FROM messaging_links
WHERE user_id = $1 AND platform = $2;

-- name: CreateMessagingLinkCode :exec
INSERT INTO messaging_link_codes (code_hash, user_id, platform, expires_at)
VALUES ($1, $2, $3, $4);

-- Issuing a code invalidates any earlier one for the same platform, so a
-- person who asks twice cannot be confused about which of two codes is live,
-- and an abandoned code stops being redeemable the moment it is replaced.
-- name: DeleteMessagingLinkCodesForUser :exec
DELETE FROM messaging_link_codes
WHERE user_id = $1 AND platform = $2;

-- RedeemMessagingLinkCode spends a code and reports whose it was.
--
-- Single use is enforced here rather than in Go: the UPDATE ... WHERE
-- redeemed_at IS NULL is atomic, so two messages arriving with the same code
-- can only produce one winner. Expiry is checked in the same predicate for the
-- same reason.
-- name: RedeemMessagingLinkCode :one
UPDATE messaging_link_codes
SET redeemed_at = now()
WHERE code_hash = $1
  AND platform = $2
  AND redeemed_at IS NULL
  AND expires_at > now()
RETURNING user_id;

-- Housekeeping for codes nobody ever sent. Spent codes are kept: the tombstone
-- is what lets a replay be recognised rather than merely missed.
-- name: DeleteExpiredMessagingLinkCodes :exec
DELETE FROM messaging_link_codes
WHERE expires_at < $1 AND redeemed_at IS NULL;
