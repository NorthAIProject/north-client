-- name: UpsertProgramme :one
-- Starting again replaces the week: a person who skipped, or finished, and
-- starts over gets day 0 back. The days are cleared separately.
INSERT INTO first_week_programmes (user_id, goal_id, template, timezone, started_on)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET goal_id     = EXCLUDED.goal_id,
    template    = EXCLUDED.template,
    timezone    = EXCLUDED.timezone,
    started_on  = EXCLUDED.started_on,
    status      = 'active',
    review      = NULL,
    created_at  = now(),
    finished_at = NULL
RETURNING *;

-- name: GetProgramme :one
SELECT * FROM first_week_programmes WHERE user_id = $1;

-- name: SetProgrammeStatus :one
-- Only a running week changes state: finishing a skipped week, or skipping a
-- finished one, changes nothing.
UPDATE first_week_programmes
SET status = $2, finished_at = now()
WHERE user_id = $1 AND status = 'active'
RETURNING *;

-- name: ListActiveProgrammes :many
SELECT * FROM first_week_programmes
WHERE status = 'active' AND user_id > $1
ORDER BY user_id
LIMIT $2;

-- name: ClearDays :exec
DELETE FROM first_week_days WHERE user_id = $1;

-- name: InsertDay :one
-- The first completion of a day is the one that counts; a second, from a
-- tap after the log was found or another channel, inserts nothing.
INSERT INTO first_week_days (user_id, day, local_date, via)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, day) DO NOTHING
RETURNING *;

-- name: ListDays :many
SELECT * FROM first_week_days WHERE user_id = $1 ORDER BY day;

-- name: SetReview :one
-- The review is written once: the coach's morning message on day 7, or the
-- catalogue's when the review is opened before that message exists. A
-- second writer keeps the first one's.
UPDATE first_week_programmes
SET review = $2
WHERE user_id = $1 AND review IS NULL
RETURNING *;
