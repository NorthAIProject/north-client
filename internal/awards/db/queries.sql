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
