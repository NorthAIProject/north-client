-- name: GetVaultConnection :one
SELECT * FROM vault_connections WHERE user_id = $1;

-- name: UpsertVaultConnection :one
INSERT INTO vault_connections (user_id, root_path, enabled, updated_at)
VALUES ($1, $2, true, now())
ON CONFLICT (user_id) DO UPDATE
SET root_path   = EXCLUDED.root_path,
    enabled     = true,
    last_error  = '',
    updated_at  = now()
RETURNING *;

-- name: DeleteVaultConnection :exec
DELETE FROM vault_connections WHERE user_id = $1;

-- name: MarkVaultSynced :exec
UPDATE vault_connections
SET last_sync_at = now(), last_error = '', updated_at = now()
WHERE user_id = $1;

-- name: MarkVaultSyncFailed :exec
UPDATE vault_connections
SET last_error = $2, updated_at = now()
WHERE user_id = $1;

-- name: ListEnabledVaultConnections :many
SELECT user_id, root_path FROM vault_connections WHERE enabled = true;
