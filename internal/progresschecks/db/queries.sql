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

-- name: OldestCheckWithPhotos :one
-- The first check a comparison starts from: the oldest with a front or side
-- photo, the poses that show a change.
SELECT c.* FROM progress_checks c
WHERE c.user_id = $1
  AND EXISTS (SELECT 1 FROM progress_check_photos p WHERE p.check_id = c.id AND p.pose IN ('front', 'side'))
ORDER BY c.measured_on ASC, c.created_at ASC
LIMIT 1;

-- name: NewestCheckWithPhotos :one
SELECT c.* FROM progress_checks c
WHERE c.user_id = $1
  AND EXISTS (SELECT 1 FROM progress_check_photos p WHERE p.check_id = c.id AND p.pose IN ('front', 'side'))
ORDER BY c.measured_on DESC, c.created_at DESC
LIMIT 1;

-- name: UpsertComparison :exec
INSERT INTO progress_check_comparisons (
    user_id, from_check_id, to_check_id, summary, confidence, limits, provider_label
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id) DO UPDATE SET
    from_check_id = EXCLUDED.from_check_id, to_check_id = EXCLUDED.to_check_id,
    summary = EXCLUDED.summary, confidence = EXCLUDED.confidence, limits = EXCLUDED.limits,
    provider_label = EXCLUDED.provider_label, created_at = now();

-- name: GetComparison :one
SELECT cmp.from_check_id, cmp.to_check_id, cmp.summary, cmp.confidence, cmp.limits,
       cmp.provider_label, cmp.created_at,
       f.measured_on AS from_on, t.measured_on AS to_on
FROM progress_check_comparisons cmp
JOIN progress_checks f ON f.id = cmp.from_check_id
JOIN progress_checks t ON t.id = cmp.to_check_id
WHERE cmp.user_id = $1;
