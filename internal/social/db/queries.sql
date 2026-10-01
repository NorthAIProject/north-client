-- name: SetHandle :one
UPDATE users SET handle = sqlc.narg('handle'), updated_at = now()
WHERE id = $1
RETURNING handle;

-- name: GetHandle :one
SELECT handle FROM users WHERE id = $1;

-- name: PersonByHandle :one
SELECT id, display_name, handle FROM users WHERE handle = $1;

-- name: PersonByID :one
SELECT id, display_name, handle FROM users WHERE id = $1;

-- name: InviteFor :one
SELECT * FROM invites WHERE inviter_id = $1 AND channel = $2;

-- name: CreateInvite :one
-- ON CONFLICT DO NOTHING returns no row, so a lost race reads the winner's
-- code with InviteFor instead of failing.
INSERT INTO invites (code, inviter_id, channel)
VALUES ($1, $2, $3)
ON CONFLICT (inviter_id, channel) DO NOTHING
RETURNING *;

-- name: InviteByCode :one
SELECT i.code, i.inviter_id, i.channel, u.display_name, u.handle
FROM invites i JOIN users u ON u.id = i.inviter_id
WHERE i.code = $1;

-- name: Redeem :one
-- The insert is the claim: one invite per new account, the first one wins.
INSERT INTO invite_redemptions (invitee_id, code)
VALUES ($1, $2)
ON CONFLICT (invitee_id) DO NOTHING
RETURNING redeemed_at;

-- name: CountRedemptions :one
SELECT count(*) FROM invite_redemptions r
JOIN invites i ON i.code = r.code
WHERE i.inviter_id = $1;

-- name: UpsertFollow :one
-- A follow request stays pending until accepted; asking again changes
-- nothing, and an accepted follow is never demoted back to pending.
INSERT INTO follows (follower_id, followee_id, status, accepted_at)
VALUES ($1, $2, sqlc.arg('status'), CASE WHEN sqlc.arg('status')::text = 'accepted' THEN now() END)
ON CONFLICT (follower_id, followee_id) DO UPDATE
SET status      = CASE WHEN follows.status = 'accepted' OR EXCLUDED.status = 'accepted' THEN 'accepted' ELSE 'pending' END,
    accepted_at = COALESCE(follows.accepted_at, EXCLUDED.accepted_at)
RETURNING *;

-- name: AcceptFollow :execrows
UPDATE follows SET status = 'accepted', accepted_at = now()
WHERE follower_id = $1 AND followee_id = $2 AND status = 'pending';

-- name: DeleteFollow :execrows
DELETE FROM follows WHERE follower_id = $1 AND followee_id = $2;

-- name: GetFollow :one
SELECT * FROM follows WHERE follower_id = $1 AND followee_id = $2;

-- name: ListFollowers :many
SELECT u.id, u.display_name, u.handle, f.status, f.created_at
FROM follows f JOIN users u ON u.id = f.follower_id
WHERE f.followee_id = $1
ORDER BY f.created_at DESC;

-- name: ListFollowing :many
SELECT u.id, u.display_name, u.handle, f.status, f.created_at
FROM follows f JOIN users u ON u.id = f.followee_id
WHERE f.follower_id = $1
ORDER BY f.created_at DESC;

-- name: CountFollows :one
SELECT
    (SELECT count(*) FROM follows f1 WHERE f1.followee_id = $1 AND f1.status = 'accepted') AS followers,
    (SELECT count(*) FROM follows f2 WHERE f2.follower_id = $1 AND f2.status = 'accepted') AS following;

-- name: Block :exec
INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: Unblock :execrows
DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2;

-- name: DeleteFollowsBetween :exec
DELETE FROM follows
WHERE (follower_id = $1 AND followee_id = $2) OR (follower_id = $2 AND followee_id = $1);

-- name: IsBlockedEitherWay :one
SELECT EXISTS (
    SELECT 1 FROM blocks
    WHERE (blocker_id = $1 AND blocked_id = $2) OR (blocker_id = $2 AND blocked_id = $1)
);

-- name: ListBlocked :many
SELECT u.id, u.display_name, u.handle
FROM blocks b JOIN users u ON u.id = b.blocked_id
WHERE b.blocker_id = $1
ORDER BY b.created_at DESC;

-- name: MatchEmailHashes :many
-- People whose email, lower-cased, hashes to one of the given SHA-256 hex
-- strings. Only accounts with a handle: choosing one is choosing to be
-- findable, which keeps this from answering "is this address on Khepri?"
-- for somebody who never asked to be found. Blocks hide either way.
SELECT u.id, u.display_name, u.handle,
       COALESCE((SELECT f.status FROM follows f WHERE f.follower_id = sqlc.arg('viewer') AND f.followee_id = u.id), '')::text AS following
FROM users u
WHERE u.handle IS NOT NULL
  AND u.id <> sqlc.arg('viewer')
  AND encode(sha256(convert_to(lower(u.email::text), 'UTF8')), 'hex') = ANY(sqlc.arg('hashes')::text[])
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = sqlc.arg('viewer') AND b.blocked_id = u.id)
                     OR (b.blocker_id = u.id AND b.blocked_id = sqlc.arg('viewer')))
ORDER BY u.display_name
LIMIT 200;
