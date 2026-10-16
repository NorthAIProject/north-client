-- name: InsertOutreach :one
-- The once-a-day claim: a second outreach for the same local day inserts
-- nothing, so two sweeps racing cannot both message the person.
INSERT INTO coach_outreach (user_id, day, trigger, facts)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, day) DO NOTHING
RETURNING *;

-- name: GetOutreachForDay :one
SELECT * FROM coach_outreach WHERE user_id = $1 AND day = $2;

-- name: GetOutreach :one
SELECT * FROM coach_outreach WHERE id = $1 AND user_id = $2;

-- name: SetOutreachMessage :exec
UPDATE coach_outreach SET body = $3, message_id = $4 WHERE id = $1 AND user_id = $2;

-- name: RecordOutreachAction :one
-- The first answer is the one that counts: a second tap, from another
-- channel or a stale notification, changes nothing.
UPDATE coach_outreach SET action = $3, acted_at = now()
WHERE id = $1 AND user_id = $2 AND action IS NULL
RETURNING *;
