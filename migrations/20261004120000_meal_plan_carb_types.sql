-- +goose Up
-- +goose StatementBegin

-- Plan-level carb type and custom carb percentage.
ALTER TABLE meal_plans
  ADD COLUMN plan_type text CHECK (plan_type IS NULL OR plan_type IN ('no_carb', 'low_carb', 'mid_carb', 'high_carb', 'custom')),
  ADD COLUMN custom_carb_pct double precision CHECK (custom_carb_pct IS NULL OR (custom_carb_pct >= 0 AND custom_carb_pct <= 100)),
  ADD COLUMN macro_plan_id uuid REFERENCES user_macro_plans (id) ON DELETE SET NULL;

-- Per-meal day assignment and per-day macro overrides.
-- weekday: NULL = unassigned; 0–6 = Sun–Sat (matches meal_reminders convention).
ALTER TABLE meals
  ADD COLUMN weekday smallint CHECK (weekday IS NULL OR (weekday >= 0 AND weekday <= 6)),
  ADD COLUMN day_plan_type text CHECK (day_plan_type IS NULL OR day_plan_type IN ('no_carb', 'low_carb', 'mid_carb', 'high_carb', 'custom')),
  ADD COLUMN day_custom_carb_g double precision CHECK (day_custom_carb_g IS NULL OR day_custom_carb_g >= 0),
  ADD COLUMN day_custom_protein_g double precision CHECK (day_custom_protein_g IS NULL OR day_custom_protein_g >= 0),
  ADD COLUMN day_custom_fat_g double precision CHECK (day_custom_fat_g IS NULL OR day_custom_fat_g >= 0),
  ADD COLUMN overage_confirmed boolean NOT NULL DEFAULT false;

-- Drop the old unique constraint on (meal_plan_id, meal_number)
ALTER TABLE meals DROP CONSTRAINT IF EXISTS meals_meal_plan_id_meal_number_key;

-- Re-add a composite uniqueness on (meal_plan_id, COALESCE(weekday, -1), meal_number)
CREATE UNIQUE INDEX meals_plan_weekday_number_uidx
  ON meals (meal_plan_id, COALESCE(weekday, -1), meal_number);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS meals_plan_weekday_number_uidx;
ALTER TABLE meals
  DROP COLUMN IF EXISTS overage_confirmed,
  DROP COLUMN IF EXISTS day_custom_fat_g,
  DROP COLUMN IF EXISTS day_custom_protein_g,
  DROP COLUMN IF EXISTS day_custom_carb_g,
  DROP COLUMN IF EXISTS day_plan_type,
  DROP COLUMN IF EXISTS weekday;
ALTER TABLE meal_plans
  DROP COLUMN IF EXISTS macro_plan_id,
  DROP COLUMN IF EXISTS custom_carb_pct,
  DROP COLUMN IF EXISTS plan_type;
ALTER TABLE meals ADD CONSTRAINT meals_meal_plan_id_meal_number_key UNIQUE (meal_plan_id, meal_number);
-- +goose StatementEnd
