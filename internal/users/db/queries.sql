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
    updated_at     = now()
WHERE id = $1
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2,
    updated_at    = now()
WHERE id = $1;

-- name: MarkUserOnboarded :one
-- Sets the first-run flag once. A second call returns the row unchanged.
UPDATE users
SET onboarded_at = now(),
    updated_at   = now()
WHERE id = $1
  AND onboarded_at IS NULL
RETURNING *;
