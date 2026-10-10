-- +goose Up
-- +goose StatementBegin

-- A food a plan offers but does not count: "compota 0% (opcional)", "meio
-- abacate (se gostar)". It is shown with its own macros and left out of the
-- meal's total, so the day and plan totals leave it out too. Every existing
-- portion is counted, as before.
ALTER TABLE meal_ingredients ADD COLUMN optional boolean NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE meal_ingredients DROP COLUMN optional;
-- +goose StatementEnd
