-- name: UpsertCheckIn :one
-- Every content column is written: the caller has already merged anything it
-- means to keep (see Service.MergeToday). source is set on insert only: it
-- records where the day's check-in was first created.
INSERT INTO check_ins (
    user_id, local_date, mood, energy, wins, challenges, notes, related_goal_id,
    source, stress, sleep_quality, tags
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
ON CONFLICT (user_id, local_date) DO UPDATE SET
    mood            = EXCLUDED.mood,
    energy          = EXCLUDED.energy,
    wins            = EXCLUDED.wins,
    challenges      = EXCLUDED.challenges,
    notes           = EXCLUDED.notes,
    related_goal_id = EXCLUDED.related_goal_id,
    stress          = EXCLUDED.stress,
    sleep_quality   = EXCLUDED.sleep_quality,
    tags            = EXCLUDED.tags,
    updated_at      = now()
RETURNING *;

-- name: GetCheckIn :one
SELECT * FROM check_ins WHERE id = $1 AND user_id = $2;

-- name: GetCheckInByDate :one
SELECT * FROM check_ins WHERE user_id = $1 AND local_date = $2;

-- name: ListCheckIns :many
SELECT * FROM check_ins
WHERE user_id = $1
ORDER BY local_date DESC
LIMIT $2;

-- name: ListCheckInsSince :many
SELECT * FROM check_ins
WHERE user_id = $1 AND local_date >= $2
ORDER BY local_date DESC
LIMIT $3;

-- name: ListCheckInsBetween :many
-- Half-open [since, until) so consecutive windows tile without overlapping.
-- Unbounded on purpose: the window itself is the limit.
SELECT * FROM check_ins
WHERE user_id = $1 AND local_date >= $2 AND local_date < $3
ORDER BY local_date DESC;

-- name: UpdateCheckIn :one
UPDATE check_ins
SET mood            = $3,
    energy          = $4,
    wins            = $5,
    challenges      = $6,
    notes           = $7,
    related_goal_id = $8,
    stress          = $9,
    sleep_quality   = $10,
    tags            = $11,
    updated_at      = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: ListCheckInDates :many
-- Newest first; used to compute streaks without loading full rows.
SELECT local_date FROM check_ins
WHERE user_id = $1
ORDER BY local_date DESC
LIMIT $2;

-- name: DeleteCheckIn :execrows
DELETE FROM check_ins WHERE id = $1 AND user_id = $2;

-- name: CountCheckIns :one
SELECT COUNT(*)::bigint FROM check_ins WHERE user_id = $1;

-- name: CheckInVersion :one
-- A cheap fingerprint of this person's check-ins for the web's live displays:
-- an edit moves the newest updated_at, a delete moves the count.
SELECT
    COUNT(*)::bigint AS total,
    COALESCE(MAX(updated_at), 'epoch'::timestamptz)::timestamptz AS latest
FROM check_ins
WHERE user_id = $1;
