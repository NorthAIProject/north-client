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
    held       = NULLIF(@held::text, ''),
    -- Any answer counts as looking back now; clearing it forgets the answer.
    held_at    = CASE WHEN @held::text = '' THEN NULL ELSE now() END,
    updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteDecision :execrows
DELETE FROM decisions WHERE id = $1 AND user_id = $2;

-- name: DueRevisit :one
-- The oldest revisit due: 30 or 90 days after the call, not yet answered
-- since that mark. Marks before since are let go, so a decision logged long
-- before revisits existed is not asked about out of nowhere.
SELECT d.id, d.title, m.mark::int AS mark
FROM decisions d
CROSS JOIN (VALUES (30), (90)) AS m (mark)
WHERE d.user_id = @user_id
  AND d.decided_at + make_interval(days => m.mark) <= @now::timestamptz
  AND d.decided_at + make_interval(days => m.mark) > @since::timestamptz
  AND (d.held_at IS NULL OR d.held_at < d.decided_at + make_interval(days => m.mark))
ORDER BY d.decided_at + make_interval(days => m.mark)
LIMIT 1;

-- name: Calibration :one
-- How the person's calls held up, among the ones they looked back on.
SELECT
    count(*) FILTER (WHERE held = 'yes')    AS held_yes,
    count(*) FILTER (WHERE held = 'partly') AS held_partly,
    count(*) FILTER (WHERE held = 'no')     AS held_no
FROM decisions
WHERE user_id = $1;
