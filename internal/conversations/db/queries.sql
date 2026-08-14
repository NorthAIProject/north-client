-- name: CreateConversation :one
INSERT INTO conversations (user_id, title)
VALUES ($1, $2)
RETURNING *;

-- name: GetConversation :one
-- Scoped by user_id, not just id. Authorisation belongs in the query: a handler
-- that forgets to check ownership then returns nothing rather than someone
-- else's conversation.
SELECT * FROM conversations
WHERE id = $1 AND user_id = $2;

-- name: ListConversations :many
SELECT * FROM conversations
WHERE user_id = $1
ORDER BY updated_at DESC
LIMIT $2 OFFSET $3;

-- name: SetConversationTitle :exec
UPDATE conversations
SET title = $2, updated_at = now()
WHERE id = $1;

-- name: TouchConversation :exec
UPDATE conversations
SET updated_at = now()
WHERE id = $1;

-- name: DeleteConversation :exec
DELETE FROM conversations
WHERE id = $1 AND user_id = $2;

-- name: AppendMessage :one
INSERT INTO messages (conversation_id, role, content, parts, usage, model, provider, evidence_refs)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListMessages :many
SELECT * FROM messages
WHERE conversation_id = $1
ORDER BY created_at
LIMIT $2;

-- name: RecentMessages :many
-- The tail of a conversation, for the context builder. Selected newest-first so
-- the limit keeps the most recent turns, then reversed in Go into reading
-- order. Ordering oldest-first with a limit would return the beginning of the
-- conversation, which is the opposite of what context needs.
SELECT * FROM messages
WHERE conversation_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: RecentUserMessages :many
-- The user's own recent messages across every conversation, so the coach has
-- continuity even in a brand new one.
SELECT m.*
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE c.user_id = $1 AND m.role = 'user'
ORDER BY m.created_at DESC
LIMIT $2;

-- name: CountMessages :one
SELECT count(*) FROM messages WHERE conversation_id = $1;

-- name: ConversationsAwaitingExtraction :many
-- Threads that have gone quiet with messages the extractor has never read.
--
-- The coach only enqueues extraction once a conversation reaches four messages,
-- which means a thread that says something worth remembering and then stops at
-- three is never mined at all. This is the other half: whatever went quiet and
-- was never looked at.
--
-- memories_extracted_at older than updated_at means new messages arrived since
-- the last pass, so the thread is worth reading again. Null means never read.
SELECT c.id, c.user_id
FROM conversations c
WHERE c.updated_at < @idle_before::timestamptz
  AND (c.memories_extracted_at IS NULL OR c.memories_extracted_at < c.updated_at)
  AND (SELECT count(*) FROM messages m WHERE m.conversation_id = c.id) >= @min_messages::bigint
ORDER BY c.updated_at
LIMIT @result_limit::int;

-- name: MarkConversationExtracted :exec
-- Records that extraction ran, not that it found anything. A thread with
-- nothing worth remembering must not be re-read on every sweep forever.
UPDATE conversations
SET memories_extracted_at = now()
WHERE id = $1;
