-- name: CreateMemory :one
INSERT INTO user_memories (
    user_id, category, content, status, pinned, source, source_conversation_id, confidence
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetMemory :one
SELECT * FROM user_memories
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: ListMemories :many
SELECT * FROM user_memories
WHERE user_id = $1
  AND deleted_at IS NULL
ORDER BY
    pinned DESC,
    CASE status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END,
    updated_at DESC
LIMIT $2;

-- name: ListMemoriesByStatus :many
SELECT * FROM user_memories
WHERE user_id = $1
  AND deleted_at IS NULL
  AND status = $2
ORDER BY pinned DESC, updated_at DESC
LIMIT $3;

-- name: ListApprovedForContext :many
SELECT * FROM user_memories
WHERE user_id = $1
  AND deleted_at IS NULL
  AND status = 'approved'
ORDER BY pinned DESC, updated_at DESC
LIMIT $2;

-- name: ListActiveContents :many
-- Used to deduplicate extractions against what is already known or proposed.
SELECT lower(trim(content)) AS content
FROM user_memories
WHERE user_id = $1
  AND deleted_at IS NULL
  AND status IN ('pending', 'approved');

-- name: UpdateMemory :one
UPDATE user_memories
SET category   = $3,
    content    = $4,
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SetMemoryStatus :one
UPDATE user_memories
SET status     = $3,
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SetMemoryPinned :one
UPDATE user_memories
SET pinned     = $3,
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL AND status = 'approved'
RETURNING *;

-- name: SoftDeleteMemory :exec
UPDATE user_memories
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: CountPendingMemories :one
SELECT count(*)::int AS count
FROM user_memories
WHERE user_id = $1 AND deleted_at IS NULL AND status = 'pending';
