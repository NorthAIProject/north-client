-- +goose Up
-- +goose StatementBegin

-- Temporal validity for durable facts.
--
-- Until now a memory was true forever. Someone who trained five days a week in
-- March and three in August had both facts in the store, both approved, both
-- reaching the coach, with nothing to say which one is current. The coach then
-- has to guess, and a coach that guesses about the person's own stated facts is
-- the one thing this product cannot be.
--
-- It matters more than it looks, because stale facts poison inference. A
-- detector that reads "trains 5x/week" from months ago and compares it against
-- this month's behaviour reports an adherence collapse that never happened —
-- and a wrong finding delivered with a citation is worse than no finding.
--
-- valid_to NULL means "still true", which keeps every existing row correct
-- without a backfill.
ALTER TABLE user_memories
    ADD COLUMN valid_to      timestamptz,
    ADD COLUMN supersedes_id uuid REFERENCES user_memories (id) ON DELETE SET NULL;

COMMENT ON COLUMN user_memories.valid_to IS
    'When this fact stopped being true. NULL means it still is.';

-- supersedes_id is a *proposal* while the row is pending and a record once it
-- is approved: the fact this one replaces.
--
-- The direction is deliberate. Pointing forward from the old row to its
-- replacement would mean writing to the old row at extraction time, before a
-- human has agreed the new fact is real — and a rejected extraction would have
-- already retired something true. Pointing back from the new row means
-- extraction only ever writes its own row, and approval is what retires
-- anything.
COMMENT ON COLUMN user_memories.supersedes_id IS
    'The memory this one replaces. Set at extraction as a proposal; acted on when this row is approved.';

-- Finding the replacement for a retired fact is a rare, human-facing question
-- ("what replaced this?"), so a partial index on the few rows that carry the
-- pointer is enough. The hot path is the opposite question — "is this current?"
-- — and that is answered by valid_to on the row itself with no lookup.
CREATE INDEX user_memories_supersedes_idx
    ON user_memories (supersedes_id)
    WHERE supersedes_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS user_memories_supersedes_idx;
ALTER TABLE user_memories
    DROP COLUMN IF EXISTS supersedes_id,
    DROP COLUMN IF EXISTS valid_to;
-- +goose StatementEnd
