-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, timezone, locale)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateUserWithID :one
-- Used when a passkey registration ceremony needs a stable user handle
-- (WebAuthnID) before the row is inserted.
INSERT INTO users (id, email, password_hash, display_name, timezone, locale)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: EmailExists :one
SELECT EXISTS (SELECT 1 FROM users WHERE email = $1);

-- name: UpdateUserProfile :one
-- A NULL appearance leaves the stored one alone. Clients built before the
-- column existed send a profile without it, and a full-replace update must not
-- read that as "forget what this person chose".
UPDATE users
SET display_name   = @display_name,
    timezone       = @timezone,
    coaching_style = @coaching_style,
    coaching_tone  = @coaching_tone,
    locale         = @locale,
    appearance     = COALESCE(sqlc.narg(appearance), appearance),
    updated_at     = now()
WHERE id = @id
RETURNING *;

-- name: SetUserAppearance :execrows
-- Writes the appearance only when it differs, so the affected-row count says
-- whether anything changed — which is what decides whether the change is
-- reported, without a read beforehand that a concurrent save could outrun.
UPDATE users
SET appearance = sqlc.arg(appearance)::text,
    updated_at = now()
WHERE id = @id
  AND appearance IS DISTINCT FROM sqlc.arg(appearance)::text;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2,
    updated_at    = now()
WHERE id = $1;

-- name: UpdateUserTier :one
-- Moves an account between plans. Whatever manages subscriptions owns when this
-- is called; the column only records the current state, with no history.
UPDATE users
SET tier       = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkUserOnboarded :one
-- Sets the first-run flag once. A second call returns the row unchanged.
UPDATE users
SET onboarded_at = now(),
    updated_at   = now()
WHERE id = $1
  AND onboarded_at IS NULL
RETURNING *;

-- name: ListOnboardedUsers :many
-- Keyset page of accounts the nudge sweep may evaluate. First-run users
-- stay silent until they finish or skip onboarding.
SELECT * FROM users
WHERE onboarded_at IS NOT NULL
  AND (@after::uuid = '00000000-0000-0000-0000-000000000000' OR id > @after)
ORDER BY id
LIMIT @result_limit;
