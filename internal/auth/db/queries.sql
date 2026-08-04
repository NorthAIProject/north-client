-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, expires_at, user_agent, ip)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSessionUser :one
-- Resolves a session to its user in one round trip. Every authenticated
-- request runs this, so splitting it into two queries would double the
-- per-request database cost for no benefit.
SELECT sqlc.embed(u), s.expires_at, s.last_seen_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.expires_at > now();

-- name: TouchSession :exec
-- Extends a session on activity, giving sliding expiry rather than a hard
-- logout mid-conversation.
UPDATE sessions
SET last_seen_at = now(),
    expires_at   = $2
WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
-- Used on password change and on explicit "sign out everywhere".
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();

-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (token_hash, user_id, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetPasswordResetToken :one
-- Unused and still within its window. Expired or already-used tokens are
-- indistinguishable from unknown ones at the service layer.
SELECT *
FROM password_reset_tokens
WHERE token_hash = $1
  AND used_at IS NULL
  AND expires_at > now();

-- name: MarkPasswordResetTokenUsed :execrows
-- Returns 0 when the token was already consumed (or never existed), so two
-- concurrent resets cannot both succeed.
UPDATE password_reset_tokens
SET used_at = now()
WHERE token_hash = $1
  AND used_at IS NULL
  AND expires_at > now();

-- name: DeletePasswordResetTokensForUser :exec
-- Called when issuing a new token (only the latest link should work) and after
-- a successful reset.
DELETE FROM password_reset_tokens WHERE user_id = $1;

-- name: DeleteExpiredPasswordResetTokens :execrows
DELETE FROM password_reset_tokens
WHERE expires_at <= now()
   OR used_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Auth identities (Google OAuth, future providers)
-- ---------------------------------------------------------------------------

-- name: CreateAuthIdentity :one
INSERT INTO auth_identities (user_id, provider, provider_subject, email)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAuthIdentity :one
SELECT * FROM auth_identities
WHERE provider = $1 AND provider_subject = $2;

-- name: ListAuthIdentitiesByUser :many
SELECT * FROM auth_identities
WHERE user_id = $1
ORDER BY created_at;

-- ---------------------------------------------------------------------------
-- WebAuthn credentials
-- ---------------------------------------------------------------------------

-- name: CreateWebAuthnCredential :one
INSERT INTO webauthn_credentials (
    credential_id, user_id, public_key, attestation_type, transport,
    sign_count, name, aaguid, backup_eligible, backup_state
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetWebAuthnCredential :one
SELECT * FROM webauthn_credentials WHERE credential_id = $1;

-- name: ListWebAuthnCredentialsByUser :many
SELECT * FROM webauthn_credentials
WHERE user_id = $1
ORDER BY created_at;

-- name: UpdateWebAuthnCredentialSignCount :exec
UPDATE webauthn_credentials
SET sign_count   = $2,
    last_used_at = now(),
    backup_eligible = $3,
    backup_state    = $4
WHERE credential_id = $1;

-- name: DeleteWebAuthnCredential :exec
DELETE FROM webauthn_credentials WHERE credential_id = $1 AND user_id = $2;

-- ---------------------------------------------------------------------------
-- WebAuthn ceremony challenges (short-lived server-side session)
-- ---------------------------------------------------------------------------

-- name: CreateWebAuthnChallenge :one
INSERT INTO webauthn_challenges (data, expires_at)
VALUES ($1, $2)
RETURNING *;

-- name: GetWebAuthnChallenge :one
SELECT * FROM webauthn_challenges
WHERE id = $1
  AND expires_at > now();

-- name: DeleteWebAuthnChallenge :exec
DELETE FROM webauthn_challenges WHERE id = $1;

-- name: DeleteExpiredWebAuthnChallenges :execrows
DELETE FROM webauthn_challenges WHERE expires_at <= now();

