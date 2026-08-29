-- ClaimMessagingUpdate resolves the sender and rejects a redelivery in one
-- statement.
--
-- These are the same question. An adapter acknowledges an update before it
-- answers it, so the platform will redeliver anything whose acknowledgement
-- was lost; doing the watermark check separately from the lookup would leave a
-- window where two deliveries of one message both pass it. The predicate on
-- last_update_id makes the advance the claim.
--
-- No rows means one of two things — not linked, or already seen — and the
-- caller distinguishes them with GetMessagingLink. Cheap, because that only
-- happens off the common path.
-- The account check is what makes a bot change safe. Update ids are a sequence
-- per bot, so a different bot's ids are not comparable with this row's
-- watermark at all — they are a new sequence that happens to start lower.
-- Without this, swapping bots makes every existing link permanently deaf, and
-- silently, because a lower id is indistinguishable from a redelivery.
--
-- IS DISTINCT FROM rather than <>: it is the form that treats a first-ever
-- claim on a legacy row the same as a genuine change.
-- name: ClaimMessagingUpdate :one
UPDATE messaging_links
SET last_update_id = $3,
    account_id     = $4,
    last_seen_at   = now()
WHERE platform = $1
  AND external_id = $2
  AND ($3 > last_update_id OR account_id IS DISTINCT FROM $4)
RETURNING *;

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
