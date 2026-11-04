-- name: GetUserAICredential :one
SELECT * FROM user_ai_credentials WHERE user_id = $1;

-- name: UpsertUserAICredential :one
INSERT INTO user_ai_credentials (user_id, provider, api_key, key_hint, model, base_url)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id) DO UPDATE
SET provider = $2, api_key = $3, key_hint = $4, model = $5, base_url = $6,
    -- A new key clears the old complaint. Leaving it would leave the page
    -- saying the credential was rejected after it had been replaced.
    --
    -- The same goes for what the last probe found: it was about what was stored
    -- then, and a stale false takes the coach's write tools away.
    last_error = '', last_error_at = NULL, supports_tools = NULL, updated_at = now()
RETURNING *;

-- UpdateUserAICredentialSettings changes the model and gateway URL without
-- touching the key, for the save that leaves the key field blank. Whether tools
-- work depends on the model and the gateway too, so the last probe's answer goes.
-- name: UpdateUserAICredentialSettings :one
UPDATE user_ai_credentials
SET model = $2, base_url = $3, last_error = '', last_error_at = NULL, supports_tools = NULL, updated_at = now()
WHERE user_id = $1
RETURNING *;

-- name: DeleteUserAICredential :execrows
DELETE FROM user_ai_credentials WHERE user_id = $1;

-- name: RecordUserAICredentialError :exec
UPDATE user_ai_credentials
SET last_error = $2, last_error_at = now()
WHERE user_id = $1;

-- RecordUserAICredentialToolSupport stores what the tools probe found.
-- Separate from the error path: a provider that cannot call tools is not a
-- broken credential, it is a working one with a limitation worth showing.
--
-- Only against the version that was probed. A probe can take minutes, and by then
-- the row may hold another provider altogether, whose answer this is not.
-- name: RecordUserAICredentialToolSupport :execrows
UPDATE user_ai_credentials
SET supports_tools = @supports_tools, updated_at = now()
WHERE user_id = @user_id AND updated_at = @saved_version;
