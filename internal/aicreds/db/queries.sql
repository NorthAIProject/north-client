-- name: GetUserAICredential :one
SELECT * FROM user_ai_credentials WHERE user_id = $1;

-- name: UpsertUserAICredential :one
INSERT INTO user_ai_credentials (user_id, provider, api_key, key_hint, model)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET provider = $2, api_key = $3, key_hint = $4, model = $5,
    -- A new key clears the old complaint. Leaving it would leave the page
    -- saying the credential was rejected after it had been replaced.
    last_error = '', last_error_at = NULL, updated_at = now()
RETURNING *;

-- UpdateUserAICredentialModel changes the model without touching the key, for
-- the save that leaves the key field blank.
-- name: UpdateUserAICredentialModel :one
UPDATE user_ai_credentials
SET model = $2, last_error = '', last_error_at = NULL, updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: DeleteUserAICredential :execrows
DELETE FROM user_ai_credentials WHERE user_id = $1;

-- name: RecordUserAICredentialError :exec
UPDATE user_ai_credentials
SET last_error = $2, last_error_at = now()
WHERE user_id = $1;
