-- name: UpsertIntegrationConnection :one
-- Connecting again replaces the endpoint and token rather than adding a second
-- row: "connect my calendar" is a statement about one calendar.
INSERT INTO integration_connections (user_id, provider, endpoint, token_sealed, status, last_error)
VALUES ($1, $2, $3, $4, 'ok', '')
ON CONFLICT (user_id, provider) DO UPDATE
SET endpoint     = EXCLUDED.endpoint,
    token_sealed = EXCLUDED.token_sealed,
    status       = 'ok',
    last_error   = '',
    updated_at   = now()
RETURNING *;

-- name: GetIntegrationConnection :one
SELECT * FROM integration_connections
WHERE user_id = $1 AND provider = $2;

-- name: DeleteIntegrationConnection :exec
DELETE FROM integration_connections
WHERE user_id = $1 AND provider = $2;

-- name: MarkIntegrationChecked :exec
-- Records the outcome of the last attempt to reach the server, so the settings
-- page can say "not reachable" instead of leaving somebody to guess.
UPDATE integration_connections
SET status          = $3,
    last_error      = $4,
    last_checked_at = now(),
    updated_at      = now()
WHERE user_id = $1 AND provider = $2;
