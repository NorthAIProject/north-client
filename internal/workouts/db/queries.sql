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

-- CreatePlan inserts both a freshly generated plan and an edited one: an edit
-- is a new row carrying its parent's intake and generation, not an UPDATE. See
-- migrations/20260827190000.
-- name: CreatePlan :one
INSERT INTO workout_plans (user_id, intake_id, name, plan, model, provider, source, edited_from)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
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

-- LatestPlanForIntake is the newest version of one plan.
--
-- The optimistic-concurrency check for editing. Scoped to the intake rather
-- than the account: "superseded" has to mean this plan changed under me, not
-- that a different plan was generated since. Comparing against the account's
-- newest row made every plan but the most recent one permanently uneditable,
-- which the plans list turned into a page of buttons that could only fail.
-- name: LatestPlanForIntake :one
SELECT * FROM workout_plans
WHERE user_id = $1 AND intake_id = $2
ORDER BY created_at DESC
LIMIT 1;

-- ListCurrentPlans returns one row per plan: the version the person is
-- currently following.
--
-- A "plan" is an intake, not a row. Every generation creates a new
-- workout_intakes row, and an edit carries its parent's intake_id forward (see
-- Service.applyEdit), so all versions of a plan share it. Grouping on intake_id
-- is what avoids walking edited_from recursively, and avoids a column that
-- would have to be kept in step.
--
-- DISTINCT ON picks each plan's latest version; the wrapper re-sorts, because
-- DISTINCT ON requires its own expression to lead the ORDER BY. The LIMIT has
-- to sit outside for the same reason: applied to a result ordered by intake_id
-- it would keep an arbitrary set of plans rather than the most recent.
-- name: ListCurrentPlans :many
SELECT * FROM (
    SELECT DISTINCT ON (intake_id) *
    FROM workout_plans
    WHERE user_id = $1
    ORDER BY intake_id, created_at DESC
) AS current_plans
ORDER BY created_at DESC
LIMIT $2;

-- name: GetActivePlan :one
-- The newest version of the plan someone chose to follow.
SELECT p.* FROM workout_plans p
JOIN workout_active_plans a ON a.intake_id = p.intake_id AND a.user_id = p.user_id
WHERE a.user_id = $1
ORDER BY p.created_at DESC
LIMIT 1;

-- name: SetActivePlan :exec
INSERT INTO workout_active_plans (user_id, intake_id)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET intake_id = EXCLUDED.intake_id, updated_at = now();

-- name: GetWeek :one
SELECT * FROM workout_weeks WHERE user_id = $1 AND week_start = $2;

-- InsertWeekIfAbsent records a default week the first time it is read. Two
-- readers racing both compute the same week; the second simply loses.
-- name: InsertWeekIfAbsent :exec
INSERT INTO workout_weeks (user_id, week_start, slots, custom)
VALUES ($1, $2, $3, false)
ON CONFLICT (user_id, week_start) DO NOTHING;

-- name: UpsertWeek :exec
INSERT INTO workout_weeks (user_id, week_start, slots, custom)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, week_start) DO UPDATE
SET slots = EXCLUDED.slots, custom = EXCLUDED.custom, updated_at = now();

-- name: DeleteWeek :exec
DELETE FROM workout_weeks WHERE user_id = $1 AND week_start = $2;

-- PreviousWeekWithIntake is the latest recorded week before week_start that
-- trained any of a plan's sessions: where that plan's rotation stopped.
-- name: PreviousWeekWithIntake :one
SELECT * FROM workout_weeks
WHERE user_id = $1
  AND week_start < $2
  AND slots @> jsonb_build_array(jsonb_build_object('intake_id', sqlc.arg(intake_id)::text))
ORDER BY week_start DESC
LIMIT 1;

-- name: ListWeeksBetween :many
SELECT * FROM workout_weeks
WHERE user_id = $1 AND week_start >= $2 AND week_start < $3
ORDER BY week_start;
