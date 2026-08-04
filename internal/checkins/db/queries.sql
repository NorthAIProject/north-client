-- name: UpsertCheckIn :one
INSERT INTO check_ins (
    user_id, local_date, mood, energy, wins, challenges, notes, related_goal_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (user_id, local_date) DO UPDATE SET
    mood            = EXCLUDED.mood,
    energy          = EXCLUDED.energy,
    wins            = EXCLUDED.wins,
    challenges      = EXCLUDED.challenges,
    notes           = EXCLUDED.notes,
    related_goal_id = EXCLUDED.related_goal_id,
    updated_at      = now()
RETURNING *;

-- name: GetCheckIn :one
SELECT * FROM check_ins WHERE id = $1 AND user_id = $2;

-- name: GetCheckInByDate :one
SELECT * FROM check_ins WHERE user_id = $1 AND local_date = $2;

-- name: ListCheckIns :many
SELECT * FROM check_ins
WHERE user_id = $1
ORDER BY local_date DESC
LIMIT $2;

-- name: ListCheckInsSince :many
SELECT * FROM check_ins
WHERE user_id = $1 AND local_date >= $2
ORDER BY local_date DESC
LIMIT $3;

-- name: UpdateCheckIn :one
UPDATE check_ins
SET mood            = $3,
    energy          = $4,
    wins            = $5,
    challenges      = $6,
    notes           = $7,
    related_goal_id = $8,
    updated_at      = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: ListCheckInDates :many
-- Newest first; used to compute streaks without loading full rows.
SELECT local_date FROM check_ins
WHERE user_id = $1
ORDER BY local_date DESC
LIMIT $2;
