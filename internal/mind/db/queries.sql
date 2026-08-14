-- name: CreateJournalEntry :one
INSERT INTO journal_entries (user_id, content, mood)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListJournalEntries :many
SELECT * FROM journal_entries
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListJournalEntriesBetween :many
-- Half-open [since, until).
SELECT * FROM journal_entries
WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
ORDER BY created_at DESC;
