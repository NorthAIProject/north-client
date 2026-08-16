-- name: ListUserStorageKeys :many
-- Every object this account owns in bucket storage, gathered before the rows
-- that name them are deleted. After that DELETE the keys are unrecoverable and
-- the bytes are orphaned forever, so this runs first and its result is the only
-- copy.
--
-- Soft-deleted documents are deliberately included: deleted_at hides a document
-- from the person, it does not remove the object, and an account being erased
-- wants the bytes gone either way.
SELECT coalesce(d.storage_key, '')::text AS storage_key
FROM documents d
WHERE d.user_id = sqlc.arg(user_id)
  AND coalesce(d.storage_key, '') <> ''
UNION ALL
SELECT m.storage_key::text
FROM media m
WHERE m.user_id = sqlc.arg(user_id)
  AND m.storage_key <> '';

-- name: DeleteUserJobs :execrows
-- Queued work naming this account. The jobs table has no user_id column, so
-- nothing cascades: left alone, a queued embedding job would keep retrying
-- against a user who no longer exists, and its payload would go on holding
-- their id long after they asked to be gone.
DELETE FROM jobs
WHERE payload ->> 'user_id' = sqlc.arg(user_id)::text;

-- name: DeleteUser :execrows
-- The erasure itself. Every foreign key that points at users is ON DELETE
-- CASCADE, so this one statement takes the account and everything hanging off
-- it — sessions included, which is what signs the person out everywhere.
DELETE FROM users
WHERE id = $1;

-- name: RecordAccountEvent :one
INSERT INTO account_events (user_id, event, detail)
VALUES ($1, $2, $3)
RETURNING *;

-- name: SetAccountEventDetail :exec
UPDATE account_events
SET detail = $2
WHERE id = $1;
