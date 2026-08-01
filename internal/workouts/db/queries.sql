-- name: CreateIntake :one
INSERT INTO workout_intakes (user_id, goal, experience, days_per_week, session_minutes, equipment, limitations)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetIntake :one
SELECT * FROM workout_intakes WHERE id = $1 AND user_id = $2;

-- name: LatestIntake :one
SELECT * FROM workout_intakes
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: CreatePlan :one
INSERT INTO workout_plans (user_id, intake_id, name, plan, model, provider)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetPlan :one
SELECT * FROM workout_plans WHERE id = $1 AND user_id = $2;

-- name: LatestPlan :one
-- Feeds the coach's context, so it can reference the plan the user is actually
-- following instead of asking them to describe it again.
SELECT * FROM workout_plans
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPlans :many
SELECT * FROM workout_plans
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;
