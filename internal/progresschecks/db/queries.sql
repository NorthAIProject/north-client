-- name: CreateCheck :one
INSERT INTO progress_checks (
    user_id, measured_on, weight_kg, weight_carried,
    waist_cm, hips_cm, chest_cm, thigh_cm, arm_cm,
    source, assist_confidence, assist_notes
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: AddCheckPhoto :exec
INSERT INTO progress_check_photos (check_id, media_id, pose)
VALUES ($1, $2, $3);

-- name: GetCheck :one
SELECT * FROM progress_checks WHERE id = $1 AND user_id = $2;

-- name: ListChecks :many
SELECT * FROM progress_checks
WHERE user_id = $1
ORDER BY measured_on DESC, created_at DESC
LIMIT $2;

-- name: ListCheckPhotos :many
-- The photos of the given checks, scoped by owner through the join so an id
-- from another account returns nothing.
SELECT p.check_id, p.media_id, p.pose
FROM progress_check_photos p
JOIN progress_checks c ON c.id = p.check_id
WHERE c.user_id = @user_id AND p.check_id = ANY(@check_ids::uuid[]);

-- name: DeleteCheck :execrows
DELETE FROM progress_checks WHERE id = $1 AND user_id = $2;

-- name: GetSchedule :one
SELECT * FROM progress_check_schedules WHERE user_id = $1;

-- name: UpsertSchedule :one
INSERT INTO progress_check_schedules (user_id, cadence, next_due_on)
VALUES ($1, $2, $3)
ON CONFLICT (user_id)
DO UPDATE SET cadence = EXCLUDED.cadence, next_due_on = EXCLUDED.next_due_on, updated_at = now()
RETURNING *;

-- name: DeleteSchedule :exec
DELETE FROM progress_check_schedules WHERE user_id = $1;
