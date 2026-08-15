-- name: CreateDecision :one
INSERT INTO decisions (user_id, title, options, rationale, outcome)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetDecision :one
SELECT * FROM decisions WHERE id = $1 AND user_id = $2;

-- name: ListDecisions :many
SELECT * FROM decisions
WHERE user_id = $1
ORDER BY decided_at DESC, created_at DESC
LIMIT $2;

-- name: UpdateDecision :one
UPDATE decisions
SET title      = $3,
    options    = $4,
    rationale  = $5,
    outcome    = $6,
    updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteDecision :execrows
DELETE FROM decisions WHERE id = $1 AND user_id = $2;
