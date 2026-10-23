-- name: Timezone :one
-- The person's zone, UTC when the stored name is empty or unknown, as
-- users.User.Location does.
SELECT COALESCE((SELECT z.name FROM pg_catalog.pg_timezone_names z WHERE z.name = u.timezone LIMIT 1), 'UTC')::text AS tz
FROM users u WHERE u.id = $1;

-- name: CountWorkouts :one
SELECT count(*)::int FROM activity_sessions WHERE user_id = $1 AND status = 'completed';

-- name: CountEarlyBird :one
SELECT count(*)::int FROM activity_sessions
WHERE user_id = $1 AND status = 'completed'
  AND EXTRACT(HOUR FROM started_at AT TIME ZONE sqlc.arg('tz')::text) < 7;

-- name: ListWorkSets :many
-- Every set that is training, all time, for the personal-record walk.
SELECT id, activity_session_id, log_date, exercise_slug, exercise_name, set_number,
       weight_kg, reps, performed_at, kind, rir
FROM set_logs
WHERE user_id = $1 AND kind IN ('work', 'drop') AND weight_kg > 0
ORDER BY performed_at, set_number;

-- name: CountCentury :one
SELECT count(*)::int FROM set_logs
WHERE user_id = $1 AND kind = 'work' AND weight_kg >= 100 AND reps >= 1;

-- name: LongestCheckInRun :one
WITH d AS (SELECT DISTINCT local_date FROM check_ins WHERE user_id = $1),
r AS (SELECT local_date - (ROW_NUMBER() OVER (ORDER BY local_date))::int AS grp FROM d)
SELECT COALESCE(max(c), 0)::int FROM (SELECT count(*) AS c FROM r GROUP BY grp) x;

-- name: LongestHabitWeekRun :one
-- A week (Monday to Sunday) is kept when one habit was completed on every
-- weekday it is scheduled for; the answer is the longest run of such weeks in
-- a row for any single habit.
WITH kept AS (
    SELECT hc.habit_id, date_trunc('week', hc.local_date)::date AS week_start
    FROM habit_completions hc
    JOIN habits h ON h.id = hc.habit_id
    WHERE hc.user_id = $1
      AND cardinality(h.days_of_week) > 0
      AND EXTRACT(DOW FROM hc.local_date)::smallint = ANY(h.days_of_week)
    GROUP BY hc.habit_id, date_trunc('week', hc.local_date), h.days_of_week
    HAVING count(DISTINCT hc.local_date) = cardinality(h.days_of_week)
),
r AS (
    SELECT habit_id,
           week_start - (ROW_NUMBER() OVER (PARTITION BY habit_id ORDER BY week_start) * 7)::int AS grp
    FROM kept
)
SELECT COALESCE(max(c), 0)::int FROM (SELECT count(*) AS c FROM r GROUP BY habit_id, grp) x;

-- name: CountHabitCompletions :one
SELECT count(*)::int FROM habit_completions WHERE user_id = $1;

-- name: LongestFoodLogRun :one
WITH d AS (SELECT DISTINCT log_date FROM food_logs WHERE user_id = $1),
r AS (SELECT log_date - (ROW_NUMBER() OVER (ORDER BY log_date))::int AS grp FROM d)
SELECT COALESCE(max(c), 0)::int FROM (SELECT count(*) AS c FROM r GROUP BY grp) x;

-- name: CountProteinDays :one
-- Days that met the current plan's protein; no current plan, none.
SELECT count(*)::int FROM (
    SELECT f.log_date FROM food_logs f
    WHERE f.user_id = $1
    GROUP BY f.log_date
    HAVING sum(f.protein_g) >= (SELECT p.protein_g FROM user_macro_plans p WHERE p.user_id = $1 AND p.is_current LIMIT 1)
       AND (SELECT p.protein_g FROM user_macro_plans p WHERE p.user_id = $1 AND p.is_current LIMIT 1) > 0
) x;

-- name: CountWaterDays :one
SELECT count(*)::int FROM (
    SELECT log_date FROM hydration_logs WHERE user_id = $1
    GROUP BY log_date HAVING sum(amount_ml) >= sqlc.arg('target_ml')::int
) x;

-- name: LongestWaterRun :one
WITH d AS (
    SELECT log_date FROM hydration_logs WHERE user_id = $1
    GROUP BY log_date HAVING sum(amount_ml) >= sqlc.arg('target_ml')::int
),
r AS (SELECT log_date - (ROW_NUMBER() OVER (ORDER BY log_date))::int AS grp FROM d)
SELECT COALESCE(max(c), 0)::int FROM (SELECT count(*) AS c FROM r GROUP BY grp) x;

-- name: CountRestedWeeks :one
-- Monday-to-Sunday weeks with seven nights of at least seven hours.
SELECT count(*)::int FROM (
    SELECT date_trunc('week', local_date) AS w FROM sleep_logs
    WHERE user_id = $1 AND duration_minutes >= 420
    GROUP BY date_trunc('week', local_date)
    HAVING count(DISTINCT local_date) >= 7
) x;

-- name: CountSleepNights :one
SELECT count(*)::int FROM sleep_logs WHERE user_id = $1;

-- name: CountJournalEntries :one
SELECT count(*)::int FROM journal_entries WHERE user_id = $1;

-- name: CountMilestones :one
SELECT count(*)::int FROM goal_milestones WHERE user_id = $1 AND status = 'completed';

-- name: CountGoalsOnTime :one
SELECT count(*)::int FROM goals
WHERE user_id = $1 AND status = 'achieved' AND target_date IS NOT NULL AND closed_at IS NOT NULL
  AND (closed_at AT TIME ZONE sqlc.arg('tz')::text)::date <= target_date;

-- name: CountWeeklyReviews :one
SELECT count(*)::int FROM weekly_focus WHERE user_id = $1 AND reviewed_at IS NOT NULL;

-- name: ProgressCadence :one
SELECT cadence FROM progress_check_schedules WHERE user_id = $1;

-- name: LongestProgressCheckRun :one
-- The longest run of checks where each is within max_gap days of the one before.
WITH d AS (SELECT DISTINCT measured_on FROM progress_checks WHERE user_id = $1),
g AS (
    SELECT measured_on,
           CASE WHEN lag(measured_on) OVER (ORDER BY measured_on) IS NULL
                  OR measured_on - lag(measured_on) OVER (ORDER BY measured_on) > sqlc.arg('max_gap')::int
                THEN 1 ELSE 0 END AS starts
    FROM d
),
r AS (SELECT sum(starts) OVER (ORDER BY measured_on) AS grp FROM g)
SELECT COALESCE(max(c), 0)::int FROM (SELECT count(*) AS c FROM r GROUP BY grp) x;

-- name: CountConnectedDays :one
SELECT count(DISTINCT day)::int FROM (
    SELECT (hm.started_at AT TIME ZONE sqlc.arg('tz')::text)::date AS day
    FROM health_metrics hm WHERE hm.user_id = sqlc.arg('user_id')::uuid AND hm.source = 'apple_health'
    UNION
    SELECT (a.started_at AT TIME ZONE sqlc.arg('tz')::text)::date AS day
    FROM activity_sessions a WHERE a.user_id = sqlc.arg('user_id')::uuid AND a.source = 'strava'
) x;

-- name: CountFasts :one
SELECT count(*)::int FROM fasting_sessions
WHERE user_id = $1 AND ended_at IS NOT NULL
  AND ended_at - started_at >= make_interval(hours => target_hours::int);

-- name: CountFollowers :one
SELECT count(*)::int FROM follows f
WHERE f.followee_id = $1 AND f.status = 'accepted'
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = f.followee_id AND b.blocked_id = f.follower_id)
         OR (b.blocker_id = f.follower_id AND b.blocked_id = f.followee_id));

-- name: CountCrewChallenges :one
SELECT count(*)::int FROM achievements WHERE user_id = $1 AND kind = 'crew_challenge_met';

-- name: InsertBadge :one
-- Once per person and badge: the unique key is the dedupe.
INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key, private)
VALUES ($1, 'awards', 'badge_earned', $2, '', $3, $4, $5)
ON CONFLICT (user_id, kind, source_key) DO NOTHING
RETURNING id;

-- name: ListBadges :many
SELECT id, source_key, occurred_at, celebrated_at FROM achievements
WHERE user_id = $1 AND kind = 'badge_earned'
ORDER BY occurred_at, source_key;

-- name: MarkBadgeSeen :exec
UPDATE achievements SET celebrated_at = now()
WHERE user_id = $1 AND kind = 'badge_earned' AND source_key = $2 AND celebrated_at IS NULL;

-- name: ActiveAreas :many
-- The areas in which the person has logged something since the given instant:
-- any row in the area's source tables stamped at or after it. The nightly
-- sweep evaluates only these, so a quiet area never earns anything from it.
SELECT area::text FROM (
    SELECT 'training' AS area WHERE EXISTS (
        SELECT 1 FROM activity_sessions WHERE user_id = sqlc.arg('user_id')::uuid
          AND (started_at >= sqlc.arg('since')::timestamptz OR ended_at >= sqlc.arg('since')::timestamptz))
    UNION ALL SELECT 'lifts' WHERE EXISTS (
        SELECT 1 FROM set_logs WHERE user_id = sqlc.arg('user_id')::uuid AND performed_at >= sqlc.arg('since')::timestamptz)
    UNION ALL SELECT 'consistency' WHERE EXISTS (
        SELECT 1 FROM check_ins WHERE user_id = sqlc.arg('user_id')::uuid
          AND (created_at >= sqlc.arg('since')::timestamptz OR updated_at >= sqlc.arg('since')::timestamptz))
    UNION ALL SELECT 'habits' WHERE EXISTS (
        SELECT 1 FROM habit_completions WHERE user_id = sqlc.arg('user_id')::uuid AND completed_at >= sqlc.arg('since')::timestamptz)
    UNION ALL SELECT 'nutrition' WHERE EXISTS (
        SELECT 1 FROM food_logs WHERE user_id = sqlc.arg('user_id')::uuid AND logged_at >= sqlc.arg('since')::timestamptz)
    UNION ALL SELECT 'hydration' WHERE EXISTS (
        SELECT 1 FROM hydration_logs WHERE user_id = sqlc.arg('user_id')::uuid AND logged_at >= sqlc.arg('since')::timestamptz)
    UNION ALL SELECT 'sleep' WHERE EXISTS (
        SELECT 1 FROM sleep_logs WHERE user_id = sqlc.arg('user_id')::uuid
          AND (created_at >= sqlc.arg('since')::timestamptz OR updated_at >= sqlc.arg('since')::timestamptz))
    UNION ALL SELECT 'mind' WHERE EXISTS (
        SELECT 1 FROM journal_entries WHERE user_id = sqlc.arg('user_id')::uuid AND created_at >= sqlc.arg('since')::timestamptz)
    UNION ALL SELECT 'goals' WHERE EXISTS (
        SELECT 1 FROM goals WHERE user_id = sqlc.arg('user_id')::uuid AND updated_at >= sqlc.arg('since')::timestamptz)
        OR EXISTS (SELECT 1 FROM goal_milestones WHERE user_id = sqlc.arg('user_id')::uuid
          AND (created_at >= sqlc.arg('since')::timestamptz OR completed_at >= sqlc.arg('since')::timestamptz))
        OR EXISTS (SELECT 1 FROM weekly_focus WHERE user_id = sqlc.arg('user_id')::uuid AND reviewed_at >= sqlc.arg('since')::timestamptz)
    UNION ALL SELECT 'body' WHERE EXISTS (
        SELECT 1 FROM health_metrics WHERE user_id = sqlc.arg('user_id')::uuid AND started_at >= sqlc.arg('since')::timestamptz)
        OR EXISTS (SELECT 1 FROM progress_checks WHERE user_id = sqlc.arg('user_id')::uuid AND created_at >= sqlc.arg('since')::timestamptz)
        OR EXISTS (SELECT 1 FROM fasting_sessions WHERE user_id = sqlc.arg('user_id')::uuid
          AND (created_at >= sqlc.arg('since')::timestamptz OR ended_at >= sqlc.arg('since')::timestamptz))
    UNION ALL SELECT 'social' WHERE EXISTS (
        SELECT 1 FROM follows WHERE followee_id = sqlc.arg('user_id')::uuid AND created_at >= sqlc.arg('since')::timestamptz)
        OR EXISTS (SELECT 1 FROM achievements WHERE user_id = sqlc.arg('user_id')::uuid
          AND kind = 'crew_challenge_met' AND created_at >= sqlc.arg('since')::timestamptz)
) a;

-- name: CountActiveUsers :one
-- People with any log in the last 90 days, across the tables a badge counts.
SELECT count(*)::int FROM users u WHERE EXISTS (SELECT 1 FROM check_ins x WHERE x.user_id = u.id AND x.created_at >= $1)
   OR EXISTS (SELECT 1 FROM food_logs x WHERE x.user_id = u.id AND x.logged_at >= $1)
   OR EXISTS (SELECT 1 FROM hydration_logs x WHERE x.user_id = u.id AND x.logged_at >= $1)
   OR EXISTS (SELECT 1 FROM sleep_logs x WHERE x.user_id = u.id AND x.created_at >= $1)
   OR EXISTS (SELECT 1 FROM set_logs x WHERE x.user_id = u.id AND x.performed_at >= $1)
   OR EXISTS (SELECT 1 FROM activity_sessions x WHERE x.user_id = u.id AND x.started_at >= $1)
   OR EXISTS (SELECT 1 FROM habit_completions x WHERE x.user_id = u.id AND x.completed_at >= $1)
   OR EXISTS (SELECT 1 FROM journal_entries x WHERE x.user_id = u.id AND x.created_at >= $1);

-- name: CountBadgeEarners :many
-- How many people hold each badge.
SELECT source_key, count(*)::int AS earned FROM achievements
WHERE kind = 'badge_earned' GROUP BY source_key;

-- name: UpsertRarity :exec
INSERT INTO badge_rarity (badge_id, earned, active, computed_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (badge_id) DO UPDATE SET earned = EXCLUDED.earned, active = EXCLUDED.active, computed_at = EXCLUDED.computed_at;

-- name: ListRarity :many
SELECT badge_id, earned, active FROM badge_rarity;

-- name: CreateMonthlyChallenge :exec
-- First writer wins; the loser reads the winner's row, so two first views
-- racing agree on one metric and one target.
INSERT INTO monthly_challenges (user_id, month, metric, target)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, month) DO NOTHING;

-- name: GetMonthlyChallenge :one
SELECT user_id, month, metric, target, created_at FROM monthly_challenges
WHERE user_id = $1 AND month = $2;

-- name: CountMonthlyWorkouts :one
-- Completed sessions whose end falls on a local date in [from_day, to_day).
SELECT count(*)::int FROM activity_sessions
WHERE user_id = sqlc.arg('user_id')::uuid AND status = 'completed'
  AND (ended_at AT TIME ZONE sqlc.arg('tz')::text)::date >= sqlc.arg('from_day')::date
  AND (ended_at AT TIME ZONE sqlc.arg('tz')::text)::date <  sqlc.arg('to_day')::date;

-- name: CountMonthlyHabitDays :one
-- Local days on which any habit was ticked, in [from_day, to_day).
SELECT count(DISTINCT local_date)::int FROM habit_completions
WHERE user_id = sqlc.arg('user_id')::uuid
  AND local_date >= sqlc.arg('from_day')::date AND local_date < sqlc.arg('to_day')::date;

-- name: CountMonthlyCheckinDays :one
SELECT count(*)::int FROM check_ins
WHERE user_id = sqlc.arg('user_id')::uuid
  AND local_date >= sqlc.arg('from_day')::date AND local_date < sqlc.arg('to_day')::date;

-- name: MonthlyHistory :one
-- What the person did in the three full local months before month_start, over
-- the months the account existed in (it was created before the month ended).
-- active_habit is whether they have a habit running now.
WITH u AS (
    SELECT created_at FROM users WHERE id = sqlc.arg('user_id')::uuid
), m AS (
    SELECT g::date AS from_day, (g + interval '1 month')::date AS to_day
    FROM generate_series(sqlc.arg('month_start')::date - interval '3 months',
                         sqlc.arg('month_start')::date - interval '1 month',
                         interval '1 month') g
), counted AS (
    SELECT
        (SELECT count(*) FROM activity_sessions a WHERE a.user_id = sqlc.arg('user_id')::uuid AND a.status = 'completed'
           AND (a.ended_at AT TIME ZONE sqlc.arg('tz')::text)::date >= m.from_day
           AND (a.ended_at AT TIME ZONE sqlc.arg('tz')::text)::date <  m.to_day) AS workouts,
        (SELECT count(DISTINCT h.local_date) FROM habit_completions h WHERE h.user_id = sqlc.arg('user_id')::uuid
           AND h.local_date >= m.from_day AND h.local_date < m.to_day) AS habit_days,
        (SELECT count(*) FROM check_ins c WHERE c.user_id = sqlc.arg('user_id')::uuid
           AND c.local_date >= m.from_day AND c.local_date < m.to_day) AS checkin_days
    FROM m, u
    WHERE (m.to_day::timestamp AT TIME ZONE sqlc.arg('tz')::text) > u.created_at
)
SELECT
    (SELECT count(*) FROM counted)::int AS months,
    COALESCE((SELECT sum(workouts) FROM counted), 0)::int AS workouts,
    COALESCE((SELECT sum(habit_days) FROM counted), 0)::int AS habit_days,
    COALESCE((SELECT sum(checkin_days) FROM counted), 0)::int AS checkin_days,
    EXISTS (SELECT 1 FROM habits WHERE user_id = sqlc.arg('user_id')::uuid AND active) AS active_habit;

-- name: InsertMonthlyMet :one
-- Once per person and month: the unique key is the dedupe.
INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key, private)
VALUES ($1, 'awards', 'monthly_challenge_met', $2, '', $3, $4, false)
ON CONFLICT (user_id, kind, source_key) DO NOTHING
RETURNING id;

-- name: GetMonthlyMetAt :one
SELECT occurred_at FROM achievements
WHERE user_id = $1 AND kind = 'monthly_challenge_met' AND source_key = $2;

-- name: ListMonthlyMedals :many
-- Met months, newest first, with the challenge each one answered.
SELECT a.id, a.source_key, a.occurred_at, c.metric, c.target
FROM achievements a
JOIN monthly_challenges c ON c.user_id = a.user_id AND c.month = (a.source_key || '-01')::date
WHERE a.user_id = $1 AND a.kind = 'monthly_challenge_met'
ORDER BY a.source_key DESC;
