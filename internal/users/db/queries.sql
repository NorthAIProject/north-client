-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, timezone)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateUserWithID :one
-- Used when a passkey registration ceremony needs a stable user handle
-- (WebAuthnID) before the row is inserted.
INSERT INTO users (id, email, password_hash, display_name, timezone)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: EmailExists :one
SELECT EXISTS (SELECT 1 FROM users WHERE email = $1);

-- name: UpdateUserProfile :one
UPDATE users
SET display_name   = $2,
    timezone       = $3,
    coaching_style = $4,
    coaching_tone  = $5,
    updated_at     = now()
WHERE id = $1
RETURNING *;

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
