-- name: CreateGoal :one
INSERT INTO goals (user_id, title, motivation, success, category, target_date)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetGoal :one
SELECT * FROM goals WHERE id = $1 AND user_id = $2;

-- name: ListGoals :many
-- Active first, then everything else, because the active ones are what the
-- person is actually doing.
SELECT * FROM goals
WHERE user_id = $1
ORDER BY (status = 'active') DESC, created_at DESC
LIMIT $2;

-- name: ListActiveGoals :many
-- Loaded on every coach message, so it is deliberately narrow.
SELECT * FROM goals
WHERE user_id = $1 AND status = 'active'
ORDER BY
    -- Goals with a deadline surface first, soonest first; open-ended goals
    -- follow. NULLS LAST is the whole point: a goal with no date is not urgent.
    target_date ASC NULLS LAST,
    created_at DESC
LIMIT $2;

-- name: UpdateGoal :one
UPDATE goals
SET title       = $3,
    motivation  = $4,
    success     = $5,
    category    = $6,
    target_date = $7,
    updated_at  = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetGoalStatus :one
UPDATE goals
SET status     = $3,
    -- Stamped when the goal leaves 'active' and cleared if it comes back, so
    -- reopening a goal does not leave it looking finished.
    closed_at  = CASE WHEN $3::text = 'active' THEN NULL ELSE now() END,
    updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteGoal :exec
DELETE FROM goals WHERE id = $1 AND user_id = $2;

-- name: AddGoalUpdate :one
INSERT INTO goal_updates (goal_id, user_id, note, progress)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListGoalUpdates :many
SELECT * FROM goal_updates
WHERE goal_id = $1 AND user_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: LatestGoalUpdates :many
-- The most recent note per goal, for the coach's context and the goal list.
SELECT DISTINCT ON (goal_id) *
FROM goal_updates
WHERE user_id = $1
ORDER BY goal_id, created_at DESC;

-- name: CreateMilestone :one
-- Position is MAX+1 for this goal so appends stay stable without a round trip.
INSERT INTO goal_milestones (goal_id, user_id, title, target_date, position)
VALUES (
    $1, $2, $3, $4,
    (SELECT COALESCE(MAX(position), -1) + 1 FROM goal_milestones WHERE goal_id = $1)
)
RETURNING *;

-- name: GetMilestone :one
SELECT * FROM goal_milestones WHERE id = $1 AND user_id = $2;

-- name: UpdateMilestone :one
UPDATE goal_milestones
SET title       = $3,
    target_date = $4,
    updated_at  = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: SetMilestoneStatus :one
UPDATE goal_milestones
SET status       = $3,
    -- Stamped when it is completed and cleared if it is reopened, so a
    -- reopened checkpoint does not still look finished.
    completed_at = CASE WHEN $3::text = 'completed' THEN now() ELSE NULL END,
    updated_at   = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteMilestone :exec
DELETE FROM goal_milestones WHERE id = $1 AND user_id = $2;

-- name: ListMilestones :many
SELECT * FROM goal_milestones
WHERE goal_id = $1 AND user_id = $2
ORDER BY position ASC, created_at ASC;

-- name: MilestoneCounts :many
-- One row per goal that has any milestones, for the list and the coach.
SELECT goal_id,
       COUNT(*)::int AS total,
       COUNT(*) FILTER (WHERE status = 'completed')::int AS completed
FROM goal_milestones
WHERE user_id = $1
GROUP BY goal_id;
