-- +goose Up
-- +goose StatementBegin

-- Editing a plan inserts a new row rather than updating one.
--
-- The table already behaved that way: queries.sql only inserts and selects,
-- every generation adds a row, and /app/training redirects to the newest. An
-- edit reuses that, which is what keeps intake_id, model and provider — all
-- NOT NULL — satisfiable without inventing an intake for a hand-edited plan.
-- It also means the model's original is never destroyed, which CLAUDE.md asks
-- for explicitly.
--
-- These two columns are the only thing missing: without them an edited plan is
-- indistinguishable from a generated one, and the chain of edits is unreadable.
ALTER TABLE workout_plans
    -- 'ai' for a plan as the model produced it, 'edited' once a person changed
    -- it. Unconstrained text, matching exercises.category: a CHECK here would
    -- need migrating the first time a third provenance appears, and the values
    -- are set by this codebase rather than by input.
    ADD COLUMN source text NOT NULL DEFAULT 'ai',

    -- The plan this one was edited from. NULL for a generated plan.
    --
    -- SET NULL rather than CASCADE: deleting an ancestor must not delete the
    -- plan someone is currently following. A broken chain is recoverable; a
    -- deleted training plan is not.
    ADD COLUMN edited_from uuid NULL REFERENCES workout_plans (id) ON DELETE SET NULL;

-- Existing rows are all model output, which the DEFAULT already gave them.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE workout_plans
    DROP COLUMN edited_from,
    DROP COLUMN source;
-- +goose StatementEnd
