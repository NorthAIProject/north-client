-- name: UpsertAPNsDevice :one
-- A re-register from the same install refreshes its topic and environment and
-- clears any earlier failure. An install that signed in as somebody else
-- moves with them.
INSERT INTO apns_devices (user_id, token, topic, environment)
VALUES ($1, $2, $3, $4)
ON CONFLICT (token) DO UPDATE
SET user_id = EXCLUDED.user_id,
    topic = EXCLUDED.topic,
    environment = EXCLUDED.environment,
    failed_at = NULL
RETURNING *;

-- name: ListAPNsDevices :many
SELECT * FROM apns_devices WHERE user_id = $1 ORDER BY created_at;

-- name: DeleteAPNsDeviceByToken :execrows
DELETE FROM apns_devices WHERE user_id = $1 AND token = $2;

-- name: DeleteAPNsDevice :exec
DELETE FROM apns_devices WHERE id = $1;

-- name: MarkAPNsDeviceUsed :exec
UPDATE apns_devices SET last_used_at = now(), failed_at = NULL WHERE id = $1;

-- name: MarkAPNsDeviceFailed :exec
UPDATE apns_devices SET failed_at = now() WHERE id = $1;
