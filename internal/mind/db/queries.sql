-- name: CreateJournalEntry :one
INSERT INTO journal_entries (user_id, content, mood)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListJournalEntries :many
SELECT * FROM journal_entries
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;
