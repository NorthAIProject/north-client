-- name: UnsetCurrentBiometrics :exec
UPDATE user_biometrics SET is_current = false WHERE user_id = $1 AND is_current;

-- name: InsertBiometric :one
INSERT INTO user_biometrics (user_id, weight_kg, height_cm, date_of_birth, sex, is_current)
VALUES ($1, $2, $3, $4, $5, true)
RETURNING *;

-- name: CurrentBiometric :one
SELECT * FROM user_biometrics WHERE user_id = $1 AND is_current;

-- name: ListBiometrics :many
SELECT * FROM user_biometrics
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;
