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
