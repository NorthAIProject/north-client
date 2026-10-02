-- name: AddItem :one
INSERT INTO inbox_items (user_id, source, text) VALUES ($1, $2, $3) RETURNING *;

-- name: GetItem :one
SELECT * FROM inbox_items WHERE id = $1 AND user_id = $2;

-- name: ListOpen :many
SELECT * FROM inbox_items
WHERE user_id = $1 AND status = 'open'
ORDER BY created_at DESC
LIMIT $2;

-- name: CountOpen :one
SELECT count(*) FROM inbox_items WHERE user_id = $1 AND status = 'open';

-- name: SetSuggestion :exec
UPDATE inbox_items SET suggestion = $3, updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'open';

-- name: MarkFiled :execrows
UPDATE inbox_items
SET status = 'filed', filed_as = $3, filed_ref = $4, updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'open';

-- name: Dismiss :execrows
UPDATE inbox_items SET status = 'dismissed', updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'open';
