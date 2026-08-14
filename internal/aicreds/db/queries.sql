-- name: GetUserAICredential :one
SELECT * FROM user_ai_credentials WHERE user_id = $1;

-- name: UpsertUserAICredential :one
INSERT INTO user_ai_credentials (user_id, provider, api_key, key_hint, model, base_url)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id) DO UPDATE
SET provider = $2, api_key = $3, key_hint = $4, model = $5, base_url = $6,
    -- A new key clears the old complaint. Leaving it would leave the page
    -- saying the credential was rejected after it had been replaced.
    last_error = '', last_error_at = NULL, updated_at = now()
RETURNING *;

-- UpdateUserAICredentialSettings changes the model and gateway URL without
-- touching the key, for the save that leaves the key field blank.
-- name: UpdateUserAICredentialSettings :one
UPDATE user_ai_credentials
SET model = $2, base_url = $3, last_error = '', last_error_at = NULL, updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: DeleteUserAICredential :execrows
DELETE FROM user_ai_credentials WHERE user_id = $1;

-- name: RecordUserAICredentialError :exec
UPDATE user_ai_credentials
SET last_error = $2, last_error_at = now()
WHERE user_id = $1;
