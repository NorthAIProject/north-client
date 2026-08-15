-- name: CreateMemory :one
INSERT INTO user_memories (
    user_id, category, content, status, pinned, excluded, source, source_conversation_id, confidence
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
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
    excluded ASC,
    CASE status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END,
    updated_at DESC
LIMIT $2;

-- name: ListMemoriesByStatus :many
SELECT * FROM user_memories
WHERE user_id = $1
  AND deleted_at IS NULL
  AND status = $2
ORDER BY pinned DESC, excluded ASC, updated_at DESC
LIMIT $3;

-- name: ListApprovedForContext :many
SELECT * FROM user_memories
WHERE user_id = $1
  AND deleted_at IS NULL
  AND status = 'approved'
  AND NOT excluded
ORDER BY pinned DESC, updated_at DESC
LIMIT $2;

-- name: SearchApprovedForContext :many
-- Approved facts ranked against what the user just said.
--
-- Pinned facts are matched by `pinned OR ...` rather than being left to the
-- ranking. A pinned fact is one the user explicitly said always matters, and
-- letting a relevance score outvote that would be the coach forgetting on
-- purpose — on the exact facts it was told never to.
--
-- The tsvector expression must stay identical to the one indexed in
-- migrations/20260808090000_add_memory_and_message_search.sql and mirrored in
-- internal/search/rank.go, or this silently becomes a sequential scan.
--
-- The query is a sentence someone said to a coach, not a search box, and that
-- changes what the operator between the terms has to be. websearch_to_tsquery
-- joins terms with AND, so "my shoulder hurts when I press overhead" only
-- matches a fact containing every one of those words — which no fact does, so
-- the whole thing retrieves nothing. Swapping AND for OR asks the question the
-- caller actually meant: which stored facts touch any of this, best first.
--
-- The swap is done on the parsed tsquery rather than on the user's text. The
-- text has already been through websearch_to_tsquery by then, so it is a
-- normalised expression of quoted lexemes, and replacing '&' cannot reach
-- inside one. Phrase (<->) and negation (!) operators are left as they are.
WITH q AS (
    SELECT replace(
        websearch_to_tsquery('english', @query::text)::text,
        '&', '|'
    )::tsquery AS tsq
)
SELECT
    m.id,
    m.category,
    m.content,
    m.pinned,
    m.updated_at,
    ts_headline(
        'english',
        m.content,
        q.tsq,
        'StartSel=[, StopSel=], MaxWords=35, MinWords=15, ShortWord=3, HighlightAll=FALSE'
    )::text AS snippet,
    ts_rank_cd(to_tsvector('english', m.content), q.tsq, 32)::float8 AS rank
FROM user_memories m, q
WHERE m.user_id = @user_id
  AND m.deleted_at IS NULL
  AND m.status = 'approved'
  AND NOT m.excluded
  AND (
      m.pinned
      OR to_tsvector('english', m.content) @@ q.tsq
  )
ORDER BY m.pinned DESC, rank DESC, m.updated_at DESC
LIMIT @result_limit;

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
    excluded   = false,
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL AND status = 'approved'
RETURNING *;

-- name: SetMemoryExcluded :one
UPDATE user_memories
SET excluded   = $3,
    pinned     = false,
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL AND status = 'approved'
RETURNING *;

-- name: SoftDeleteMemory :execrows
UPDATE user_memories
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: CountPendingMemories :one
SELECT count(*)::int AS count
FROM user_memories
WHERE user_id = $1 AND deleted_at IS NULL AND status = 'pending';

-- name: ListPendingForConversation :many
SELECT * FROM user_memories
WHERE user_id = $1
  AND source_conversation_id = $2
  AND status = 'pending'
  AND deleted_at IS NULL
ORDER BY created_at DESC;
