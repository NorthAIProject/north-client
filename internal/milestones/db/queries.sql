-- name: CreateMilestone :one
INSERT INTO milestone_trackers (user_id, name, last_done_on, interval_months)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: MarkMilestoneDone :one
UPDATE milestone_trackers SET last_done_on = $3
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteMilestone :execrows
DELETE FROM milestone_trackers WHERE id = $1 AND user_id = $2;

-- name: ListMilestones :many
SELECT * FROM milestone_trackers WHERE user_id = $1 ORDER BY created_at, name;
