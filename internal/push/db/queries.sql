-- name: UpsertPushSubscription :one
-- A re-subscribe from the same browser replaces the keys and clears any
-- earlier failure. A browser that signed in as somebody else moves with them.
INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (endpoint) DO UPDATE
SET user_id    = EXCLUDED.user_id,
    p256dh     = EXCLUDED.p256dh,
    auth       = EXCLUDED.auth,
    user_agent = EXCLUDED.user_agent,
    failed_at  = NULL
RETURNING *;

-- name: ListPushSubscriptions :many
SELECT * FROM push_subscriptions
WHERE user_id = $1
ORDER BY created_at;

-- name: CountPushSubscriptions :one
SELECT count(*)::int FROM push_subscriptions
WHERE user_id = $1;

-- name: DeletePushSubscriptionByEndpoint :execrows
DELETE FROM push_subscriptions
WHERE user_id = $1 AND endpoint = $2;

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions
WHERE id = $1;

-- name: MarkPushSubscriptionUsed :exec
UPDATE push_subscriptions
SET last_used_at = now(),
    failed_at    = NULL
WHERE id = $1;

-- name: MarkPushSubscriptionFailed :exec
UPDATE push_subscriptions
SET failed_at = now()
WHERE id = $1;
