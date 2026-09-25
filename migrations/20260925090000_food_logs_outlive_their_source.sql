-- +goose Up
-- +goose StatementBegin

-- A food log entry is a snapshot: its label and macros are copied when it is
-- logged, so it stays true after the meal plan or ingredient it came from is
-- deleted. Both references are ON DELETE SET NULL for exactly that reason,
-- but the check below demanded exactly one of them, so SET NULL violated it
-- and deleting a meal plan that had ever been logged failed outright (as did
-- deleting a logged ingredient of one's own).
--
-- An entry now names at most one source; none means the source is gone.
ALTER TABLE food_logs DROP CONSTRAINT food_logs_check;
ALTER TABLE food_logs ADD CONSTRAINT food_logs_one_source_at_most
    CHECK (num_nonnulls(meal_id, ingredient_id) <= 1);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Rows whose source was deleted cannot satisfy the old check; they are kept
-- rather than lost, so the old constraint returns without validating them.
ALTER TABLE food_logs DROP CONSTRAINT food_logs_one_source_at_most;
ALTER TABLE food_logs ADD CONSTRAINT food_logs_check
    CHECK (num_nonnulls(meal_id, ingredient_id) = 1) NOT VALID;
-- +goose StatementEnd
