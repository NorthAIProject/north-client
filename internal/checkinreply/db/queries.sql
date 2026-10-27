-- name: GetReply :one
SELECT * FROM check_in_replies WHERE check_in_id = $1 AND user_id = $2;

-- name: UpsertReply :exec
-- The note is rewritten when the check-in it answers changed.
INSERT INTO check_in_replies (check_in_id, user_id, content_hash, kind, text)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (check_in_id) DO UPDATE SET
    content_hash = EXCLUDED.content_hash,
    kind         = EXCLUDED.kind,
    text         = EXCLUDED.text,
    updated_at   = now();
