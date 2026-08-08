-- +goose Up

-- Full-text search over the two things North already remembers.
--
-- Until now the coach's memory was read by recency alone —
-- `ORDER BY pinned, updated_at LIMIT 20` — so what the user actually asked
-- never influenced what the model was shown. These indexes are what make
-- retrieval answer the question instead of reciting the last twenty rows.
--
-- Expression indexes, not a generated `search tsvector` column, on purpose.
-- Both tables are read with `SELECT *` throughout (see
-- internal/memories/db/queries.sql and internal/conversations/db/queries.sql),
-- so a real column would appear in every generated row struct and would need a
-- pgx codec for tsvector that pgx/v5 does not ship. Nothing ever wants to read
-- the vector — only to match against it — so it does not need to be a column.
--
-- The cost is that every query must spell the expression exactly as written
-- here or the planner will not use the index. That is why the fragment lives in
-- exactly one place in Go: internal/search/rank.go.
--
-- 'english' rather than 'simple': stemming is what makes "training" find
-- "trained". It is wrong for a user who writes in another language, and that
-- is a real limitation to revisit when North has such a user.

CREATE INDEX idx_user_memories_search
    ON user_memories
    USING GIN (to_tsvector('english', content))
    WHERE deleted_at IS NULL;

CREATE INDEX idx_messages_search
    ON messages
    USING GIN (to_tsvector('english', content));

-- Which stored facts produced a reply.
--
-- The coach cites its context with refs like `memory:<uuid>`; the service
-- strips them from the visible text and records them here. Without this a
-- reply is unfalsifiable — there is no way, afterwards, to tell what the model
-- was actually working from.
ALTER TABLE messages
    ADD COLUMN evidence_refs text[] NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE messages DROP COLUMN evidence_refs;
DROP INDEX idx_messages_search;
DROP INDEX idx_user_memories_search;
