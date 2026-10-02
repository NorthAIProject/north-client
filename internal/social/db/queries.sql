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

-- name: MatchContactHashes :many
-- People whose email, lower-cased, or verified phone number, E.164, hashes
-- to one of the given SHA-256 hex strings. Only accounts with a handle:
-- choosing one is choosing to be findable, which keeps this from answering
-- "is this address on Khepri?" for somebody who never asked to be found. An
-- unverified number matches nothing. Blocks hide either way.
SELECT u.id, u.display_name, u.handle,
       COALESCE((SELECT f.status FROM follows f WHERE f.follower_id = sqlc.arg('viewer') AND f.followee_id = u.id), '')::text AS following
FROM users u
WHERE u.handle IS NOT NULL
  AND u.id <> sqlc.arg('viewer')
  AND (encode(sha256(convert_to(lower(u.email::text), 'UTF8')), 'hex') = ANY(sqlc.arg('email_hashes')::text[])
       OR (u.phone_verified_at IS NOT NULL
           AND encode(sha256(convert_to(u.phone_e164, 'UTF8')), 'hex') = ANY(sqlc.arg('phone_hashes')::text[])))
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = sqlc.arg('viewer') AND b.blocked_id = u.id)
                     OR (b.blocker_id = u.id AND b.blocked_id = sqlc.arg('viewer')))
ORDER BY u.display_name
LIMIT 200;

-- ---------------------------------------------------------------------------
-- Phone numbers
-- ---------------------------------------------------------------------------

-- name: GetPhone :one
SELECT phone_e164, phone_verified_at FROM users WHERE id = $1;

-- name: LockUserForPhone :exec
-- Serialises one account's starts so two at once cannot both slip under
-- the rate limit.
SELECT 1 FROM users WHERE id = $1 FOR UPDATE;

-- name: CountPhoneStartsByUser :one
SELECT count(*) FROM phone_verifications
WHERE user_id = $1 AND created_at > sqlc.arg('since');

-- name: CountPhoneStartsByNumber :one
SELECT count(*) FROM phone_verifications
WHERE phone_e164 = $1 AND created_at > sqlc.arg('since');

-- name: ClosePhoneVerifications :exec
UPDATE phone_verifications SET closed_at = now()
WHERE user_id = $1 AND closed_at IS NULL;

-- name: CreatePhoneVerification :one
INSERT INTO phone_verifications (user_id, phone_e164)
VALUES ($1, $2)
RETURNING *;

-- name: ClosePhoneVerification :exec
UPDATE phone_verifications SET closed_at = now() WHERE id = $1;

-- name: PendingPhoneVerification :one
SELECT * FROM phone_verifications
WHERE user_id = $1 AND closed_at IS NULL AND created_at > sqlc.arg('since')
ORDER BY created_at DESC
LIMIT 1;

-- name: CountPhoneCheck :one
-- Counted before the code is checked, so parallel guesses cannot outrun
-- the limit.
UPDATE phone_verifications SET attempts = attempts + 1
WHERE id = $1
RETURNING attempts;

-- name: PrunePhoneVerifications :exec
DELETE FROM phone_verifications WHERE created_at < sqlc.arg('before');

-- name: ReleasePhone :exec
-- The number leaves any other account that held it: the latest account to
-- prove possession wins.
UPDATE users SET phone_e164 = NULL, phone_verified_at = NULL, updated_at = now()
WHERE phone_e164 = sqlc.arg('phone') AND id <> sqlc.arg('keeper');

-- name: SetPhone :exec
UPDATE users SET phone_e164 = sqlc.arg('phone'), phone_verified_at = now(), updated_at = now()
WHERE id = $1;

-- name: ClearPhone :exec
UPDATE users SET phone_e164 = NULL, phone_verified_at = NULL, updated_at = now()
WHERE id = $1;

-- ---------------------------------------------------------------------------
-- Facebook
--
-- The link is an auth_identities row with provider 'facebook', the same
-- (provider, subject) shape Google and Apple use. Only this package writes
-- facebook rows, and nothing signs in with them: there is no Facebook
-- sign-in, so removing the row can never lock anybody out.
-- ---------------------------------------------------------------------------

-- name: FacebookOwner :one
SELECT user_id FROM auth_identities
WHERE provider = 'facebook' AND provider_subject = $1;

-- name: IsFacebookConnected :one
SELECT EXISTS (SELECT 1 FROM auth_identities WHERE user_id = $1 AND provider = 'facebook');

-- name: UnlinkFacebook :exec
DELETE FROM auth_identities WHERE user_id = $1 AND provider = 'facebook';

-- name: LinkFacebook :one
-- ON CONFLICT DO NOTHING returns no row when another account took the id
-- first; the caller reads the owner again.
INSERT INTO auth_identities (user_id, provider, provider_subject)
VALUES ($1, 'facebook', sqlc.arg('subject'))
ON CONFLICT (provider, provider_subject) DO NOTHING
RETURNING user_id;

-- name: FacebookFriendsHere :many
-- Accounts linked to any of these Facebook ids that the viewer may be
-- shown: a handle, not themselves, no block either way.
SELECT u.id
FROM auth_identities ai JOIN users u ON u.id = ai.user_id
WHERE ai.provider = 'facebook'
  AND ai.provider_subject = ANY(sqlc.arg('subjects')::text[])
  AND u.handle IS NOT NULL
  AND u.id <> sqlc.arg('viewer')
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = sqlc.arg('viewer') AND b.blocked_id = u.id)
                     OR (b.blocker_id = u.id AND b.blocked_id = sqlc.arg('viewer')))
LIMIT 200;

-- name: PeopleByIDs :many
-- The same filter again at read time: in the hour an import lives, somebody
-- may clear their handle or block the viewer.
SELECT u.id, u.display_name, u.handle,
       COALESCE((SELECT f.status FROM follows f WHERE f.follower_id = sqlc.arg('viewer') AND f.followee_id = u.id), '')::text AS following
FROM users u
WHERE u.id = ANY(sqlc.arg('ids')::uuid[])
  AND u.handle IS NOT NULL
  AND u.id <> sqlc.arg('viewer')
  AND NOT EXISTS (SELECT 1 FROM blocks b
                  WHERE (b.blocker_id = sqlc.arg('viewer') AND b.blocked_id = u.id)
                     OR (b.blocker_id = u.id AND b.blocked_id = sqlc.arg('viewer')))
ORDER BY u.display_name;

-- name: SaveFacebookImport :exec
INSERT INTO facebook_imports (user_id, people, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
SET people = EXCLUDED.people, imported_at = now(), expires_at = EXCLUDED.expires_at;

-- name: FacebookImport :one
SELECT people, imported_at FROM facebook_imports
WHERE user_id = $1 AND expires_at > now();

-- name: DeleteFacebookImport :exec
DELETE FROM facebook_imports WHERE user_id = $1;

-- name: CreateFacebookOAuthState :exec
INSERT INTO facebook_oauth_states (state_hash, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: TakeFacebookOAuthState :one
-- Single use: the row is gone whether or not the connection then succeeds.
DELETE FROM facebook_oauth_states
WHERE state_hash = $1 AND expires_at > now()
RETURNING user_id;

-- name: DeleteExpiredFacebookOAuthStates :exec
DELETE FROM facebook_oauth_states WHERE expires_at <= now();
