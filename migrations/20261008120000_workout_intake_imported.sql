-- +goose Up
-- +goose StatementBegin

-- A plan imported from a file has no intake: nobody answered the form. It still
-- needs a workout_intakes row, because intake_id is NOT NULL and is what groups
-- a plan's versions (see ListCurrentPlans). This flag marks that row as
-- bookkeeping rather than answers, so it is never used to pre-fill the next
-- intake, never supplies equipment for a swap, and never has a plan checked
-- against it.
ALTER TABLE workout_intakes
    ADD COLUMN imported boolean NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE workout_intakes DROP COLUMN imported;
-- +goose StatementEnd
