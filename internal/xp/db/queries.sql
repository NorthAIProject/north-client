-- name: Earned :many
-- What each person earned in a window, by kind. Nothing here is stored: every
-- row is counted from the slice that owns it, under the rules in internal/xp.
-- Dates are local days (first inclusive, last exclusive); times bound the
-- kinds that carry a timestamp. A streak day needs the check-ins before the
-- window too, so that scan is open at the start.
WITH people AS (
    -- An unknown zone name falls back to UTC, as users.User.Location does,
    -- rather than failing the whole board.
    SELECT id, COALESCE((SELECT z.name FROM pg_catalog.pg_timezone_names z WHERE z.name = users.timezone LIMIT 1), 'UTC') AS timezone
    FROM users WHERE id = ANY(sqlc.arg('user_ids')::uuid[])
),
habits_kept AS (
    SELECT hc.user_id, count(*)::int AS n
    FROM habit_completions hc
    JOIN habits h ON h.id = hc.habit_id
    WHERE hc.user_id IN (SELECT id FROM people)
      AND hc.local_date >= (sqlc.arg('from_day')::text)::date
      AND hc.local_date <  (sqlc.arg('to_day')::text)::date
      AND EXTRACT(DOW FROM hc.local_date)::smallint = ANY(h.days_of_week)
    GROUP BY hc.user_id
),
runs AS (
    SELECT user_id, local_date,
           local_date - (ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY local_date))::int AS run
    FROM check_ins
    WHERE user_id IN (SELECT id FROM people)
      AND local_date < (sqlc.arg('to_day')::text)::date
),
streak_days AS (
    SELECT user_id, count(*)::int AS n
    FROM (
        SELECT user_id, local_date,
               ROW_NUMBER() OVER (PARTITION BY user_id, run ORDER BY local_date) AS day_in_run
        FROM runs
    ) d
    WHERE day_in_run >= sqlc.arg('streak_from')::int
      AND local_date >= (sqlc.arg('from_day')::text)::date
    GROUP BY user_id
),
workouts AS (
    SELECT user_id, count(*)::int AS n
    FROM (
        SELECT s.user_id,
               ROW_NUMBER() OVER (
                   PARTITION BY s.user_id, (s.ended_at AT TIME ZONE p.timezone)::date
                   ORDER BY s.ended_at
               ) AS nth
        FROM activity_sessions s
        JOIN people p ON p.id = s.user_id
        WHERE s.status = 'completed'
          AND s.ended_at >= sqlc.arg('from_at')::timestamptz
          AND s.ended_at <  sqlc.arg('to_at')::timestamptz
          AND EXTRACT(EPOCH FROM (s.ended_at - s.started_at)) - s.total_paused_seconds
              >= sqlc.arg('workout_min_seconds')::int
    ) w
    WHERE nth <= sqlc.arg('workouts_per_day')::int
    GROUP BY user_id
),
milestones AS (
    SELECT user_id, count(*)::int AS n
    FROM goal_milestones
    WHERE user_id IN (SELECT id FROM people)
      AND status = 'completed'
      AND completed_at >= sqlc.arg('from_at')::timestamptz AND completed_at < sqlc.arg('to_at')::timestamptz
    GROUP BY user_id
),
goals_achieved AS (
    SELECT user_id, count(*)::int AS n
    FROM goals
    WHERE user_id IN (SELECT id FROM people)
      AND status = 'achieved'
      AND closed_at >= sqlc.arg('from_at')::timestamptz AND closed_at < sqlc.arg('to_at')::timestamptz
    GROUP BY user_id
)
SELECT user_id, 'habit_kept'::text AS kind, n FROM habits_kept
UNION ALL SELECT user_id, 'streak_day', n FROM streak_days
UNION ALL SELECT user_id, 'workout', n FROM workouts
UNION ALL SELECT user_id, 'milestone', n FROM milestones
UNION ALL SELECT user_id, 'goal', n FROM goals_achieved;

-- name: Participants :many
-- Who appears on the viewer's board for one sharing category: the viewer, and
-- everyone they follow (accepted) who shares that category, minus anybody
-- blocked either way. The same rule as the friends feed.
SELECT u.id, u.display_name, u.handle, u.timezone
FROM users u
WHERE u.id = sqlc.arg('viewer')
   OR (
     EXISTS (SELECT 1 FROM follows f
             WHERE f.follower_id = sqlc.arg('viewer') AND f.followee_id = u.id AND f.status = 'accepted')
     AND EXISTS (SELECT 1 FROM achievement_sharing s
                 WHERE s.user_id = u.id AND s.category = sqlc.arg('category') AND s.shared)
     AND NOT EXISTS (SELECT 1 FROM blocks b
                     WHERE (b.blocker_id = sqlc.arg('viewer') AND b.blocked_id = u.id)
                        OR (b.blocker_id = u.id AND b.blocked_id = sqlc.arg('viewer')))
   );

-- name: Shares :one
-- Whether a person shares one category with followers.
SELECT EXISTS (
    SELECT 1 FROM achievement_sharing
    WHERE user_id = sqlc.arg('user_id') AND category = sqlc.arg('category') AND shared
);

-- name: WorkoutsFinished :many
-- Finished sessions per person in a window, uncapped: the workouts board
-- counts what was done, like a crew's board does.
SELECT user_id, count(*)::int AS n
FROM activity_sessions
WHERE user_id = ANY(sqlc.arg('user_ids')::uuid[])
  AND status = 'completed'
  AND ended_at >= sqlc.arg('from_at')::timestamptz AND ended_at < sqlc.arg('to_at')::timestamptz
GROUP BY user_id;
