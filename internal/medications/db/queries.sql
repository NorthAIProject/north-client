-- name: CreateMedication :one
INSERT INTO medications (user_id, name, dose, times_of_day, days_of_week, remind, notes)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateMedication :one
UPDATE medications
SET name = $3, dose = $4, times_of_day = $5, days_of_week = $6, remind = $7, notes = $8, updated_at = now()
WHERE id = $1 AND user_id = $2 AND stopped_at IS NULL
RETURNING *;

-- name: StopMedication :one
UPDATE medications
SET stopped_at = $3, updated_at = now()
WHERE id = $1 AND user_id = $2 AND stopped_at IS NULL
RETURNING *;

-- name: GetMedication :one
SELECT * FROM medications WHERE id = $1 AND user_id = $2;

-- name: ListActiveMedications :many
SELECT * FROM medications
WHERE user_id = $1 AND stopped_at IS NULL
ORDER BY lower(name);

-- name: ListAllMedications :many
SELECT * FROM medications
WHERE user_id = $1
ORDER BY stopped_at IS NOT NULL, lower(name), created_at;

-- name: UpsertMedicationLog :one
-- A scheduled slot answered twice keeps the second answer. A NULL slot never
-- conflicts, so as-needed doses always insert.
INSERT INTO medication_logs (user_id, medication_id, log_date, slot, status, logged_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (medication_id, log_date, slot)
DO UPDATE SET status = EXCLUDED.status, logged_at = EXCLUDED.logged_at
RETURNING *;

-- name: DeleteMedicationLog :execrows
DELETE FROM medication_logs WHERE id = $1 AND user_id = $2;

-- name: ListMedicationLogsOn :many
SELECT l.*, m.name AS medication_name, m.dose AS medication_dose
FROM medication_logs l
JOIN medications m ON m.id = l.medication_id
WHERE l.user_id = $1 AND l.log_date = $2
ORDER BY l.logged_at DESC;

-- name: ListAllMedicationLogs :many
SELECT l.*, m.name AS medication_name, m.dose AS medication_dose
FROM medication_logs l
JOIN medications m ON m.id = l.medication_id
WHERE l.user_id = $1
ORDER BY l.log_date DESC, l.logged_at DESC;
