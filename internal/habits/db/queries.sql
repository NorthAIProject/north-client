-- name: CreateHabit :one
INSERT INTO habits (user_id, name, domain, days_of_week)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateHabit :one
UPDATE habits
SET name = $3, domain = $4, days_of_week = $5, active = $6, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: GetHabit :one
SELECT * FROM habits WHERE id = $1 AND user_id = $2;

-- name: ListHabits :many
SELECT * FROM habits
WHERE user_id = $1 AND (active OR NOT sqlc.arg(active_only)::boolean)
ORDER BY created_at;

-- name: DeleteHabit :exec
DELETE FROM habits WHERE id = $1 AND user_id = $2;

-- name: CompleteHabit :exec
INSERT INTO habit_completions (habit_id, user_id, local_date)
VALUES ($1, $2, $3)
ON CONFLICT (habit_id, local_date) DO NOTHING;

-- name: UncompleteHabit :exec
DELETE FROM habit_completions
WHERE habit_id = $1 AND user_id = $2 AND local_date = $3;

-- name: ListCompletionDatesSince :many
SELECT habit_id, local_date
FROM habit_completions
WHERE user_id = $1 AND local_date >= $2
ORDER BY local_date DESC;

-- name: ListCompletionsBetween :many
-- Half-open [since, until). Ordered by completed_at rather than local_date so
-- the activity timeline can place a habit against the rest of its day, which
-- local_date alone cannot do.
SELECT * FROM habit_completions
WHERE user_id = $1 AND local_date >= $2 AND local_date < $3
ORDER BY completed_at DESC;
