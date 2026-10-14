-- +goose Up
-- +goose StatementBegin

-- A meal the plan offers but does not count: "Ceia (opcional)", a pre-workout
-- snack eaten only on some days. It is shown with its own total and left out
-- of the day's and the plan's totals. The flag belongs to the slot, so every
-- option of a slot carries the same value. Every existing meal is counted, as
-- before.
ALTER TABLE meals ADD COLUMN optional boolean NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE meals DROP COLUMN optional;
-- +goose StatementEnd
