-- +goose Up
-- +goose StatementBegin

-- A meal slot holds interchangeable options: "Almoço" is chicken and rice, or
-- fish and potatoes. Each option is a meals row; options of one slot share the
-- slot's meal_number, and option 1 is the default — the one a day's totals and
-- its overage check count. Existing meals become their slot's only option.
ALTER TABLE meals
    ADD COLUMN option_index smallint NOT NULL DEFAULT 1 CHECK (option_index >= 1),
    ADD COLUMN option_label text NOT NULL DEFAULT '',
    DROP CONSTRAINT meals_day_number_key,
    ADD CONSTRAINT meals_day_slot_option_key UNIQUE (day_id, meal_number, option_index);

-- An imported food line keeps what the file said, and is marked when its
-- catalog food and grams were the importer's estimate rather than a match.
ALTER TABLE meal_ingredients
    ADD COLUMN source_text text NOT NULL DEFAULT '',
    ADD COLUMN estimated boolean NOT NULL DEFAULT false;

-- Free text an imported plan carried beside its meals.
ALTER TABLE meal_plans ADD COLUMN notes text NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Only defaults survive; their ingredients go with them through
-- meal_ingredients' ON DELETE CASCADE. Plan totals never counted alternatives,
-- so they need no recompute.
DELETE FROM meals WHERE option_index > 1;

ALTER TABLE meal_plans DROP COLUMN notes;

ALTER TABLE meal_ingredients
    DROP COLUMN estimated,
    DROP COLUMN source_text;

ALTER TABLE meals
    DROP CONSTRAINT meals_day_slot_option_key,
    DROP COLUMN option_label,
    DROP COLUMN option_index,
    ADD CONSTRAINT meals_day_number_key UNIQUE (day_id, meal_number);

-- +goose StatementEnd
