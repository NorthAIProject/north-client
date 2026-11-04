-- name: GetAIConsent :one
SELECT user_id, status, version, decided_at
FROM ai_consents
WHERE user_id = $1;

-- name: SetAIConsent :one
INSERT INTO ai_consents (user_id, status, version, decided_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id) DO UPDATE
SET status = EXCLUDED.status, version = EXCLUDED.version, decided_at = now()
RETURNING user_id, status, version, decided_at;
