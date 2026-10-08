-- +goose Up
-- +goose StatementBegin

-- A plan's days become rows of their own. 20261004120000 modelled a day as a
-- weekday copied onto every meal, with the day's carb overrides repeated per
-- meal, so two meals on the same day could disagree about the day's target.
-- Here a day holds its overrides once and its meals point at it.
CREATE TABLE meal_plan_days (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    meal_plan_id uuid        NOT NULL REFERENCES meal_plans (id) ON DELETE CASCADE,

    -- time.Weekday numbering, 0 = Sunday, as meal_reminders and habits use.
    weekday      smallint    NOT NULL CHECK (weekday BETWEEN 0 AND 6),

    -- Advanced-mode overrides of the plan's default target. A day's carbs come
    -- from a preset band (a lower- or higher-carb day) or from grams, never
    -- both. NULL everywhere means the day follows the plan.
    carb_type    text        CHECK (carb_type IN ('no_carb', 'low_carb', 'mid_carb', 'high_carb')),
    carb_g       double precision CHECK (carb_g >= 0),
    protein_g    double precision CHECK (protein_g >= 0),
    fat_g        double precision CHECK (fat_g >= 0),

    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (meal_plan_id, weekday),
    -- Target of meals' composite foreign key, which keeps a meal's plan and
    -- its day's plan the same.
    UNIQUE (id, meal_plan_id),
    CONSTRAINT meal_plan_days_one_carb_override CHECK (carb_type IS NULL OR carb_g IS NULL)
);

-- Every plan now has a carb type. Plans made before there was one, and custom
-- plans saved without a percentage, take the mid band.
UPDATE meal_plans SET plan_type = 'mid_carb'
WHERE plan_type IS NULL OR (plan_type = 'custom' AND custom_carb_pct IS NULL);
UPDATE meal_plans SET custom_carb_pct = NULL WHERE plan_type <> 'custom';

-- Existing plans start in advanced mode, so a day that is already over its
-- target can be confirmed rather than locking the plan.
ALTER TABLE meal_plans
    ALTER COLUMN plan_type SET NOT NULL,
    ADD COLUMN mode text NOT NULL DEFAULT 'advanced' CHECK (mode IN ('easy', 'advanced')),
    ADD CONSTRAINT meal_plans_custom_pct_matches_type CHECK ((plan_type = 'custom') = (custom_carb_pct IS NOT NULL)),
    -- Never read: targets come from the person's current macro plan.
    DROP COLUMN macro_plan_id;
ALTER TABLE meal_plans ALTER COLUMN mode DROP DEFAULT;

-- One day per weekday the plan's meals were on; meals with no weekday, and
-- plans with no meals, land on Monday.
INSERT INTO meal_plan_days (meal_plan_id, weekday)
SELECT DISTINCT meal_plan_id, COALESCE(weekday, 1) FROM meals;

INSERT INTO meal_plan_days (meal_plan_id, weekday)
SELECT p.id, 1 FROM meal_plans p
WHERE NOT EXISTS (SELECT 1 FROM meal_plan_days d WHERE d.meal_plan_id = p.id);

ALTER TABLE meals ADD COLUMN day_id uuid;

UPDATE meals m SET day_id = d.id
FROM meal_plan_days d
WHERE d.meal_plan_id = m.meal_plan_id AND d.weekday = COALESCE(m.weekday, 1);

-- Meals that shared a number on different weekdays may now share a day.
DROP INDEX meals_plan_weekday_number_uidx;
UPDATE meals m SET meal_number = r.n
FROM (SELECT id, row_number() OVER (PARTITION BY day_id ORDER BY meal_number, created_at) AS n FROM meals) r
WHERE r.id = m.id;

ALTER TABLE meals
    ALTER COLUMN day_id SET NOT NULL,
    ADD CONSTRAINT meals_day_fk FOREIGN KEY (day_id, meal_plan_id)
        REFERENCES meal_plan_days (id, meal_plan_id) ON DELETE CASCADE,
    ADD CONSTRAINT meals_day_number_key UNIQUE (day_id, meal_number),
    DROP COLUMN weekday,
    DROP COLUMN day_plan_type,
    DROP COLUMN day_custom_carb_g,
    DROP COLUMN day_custom_protein_g,
    DROP COLUMN day_custom_fat_g,
    DROP COLUMN overage_confirmed;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE meal_plans
    ADD COLUMN macro_plan_id uuid REFERENCES user_macro_plans (id) ON DELETE SET NULL,
    DROP CONSTRAINT meal_plans_custom_pct_matches_type,
    ALTER COLUMN plan_type DROP NOT NULL,
    DROP COLUMN mode;

ALTER TABLE meals
    ADD COLUMN weekday smallint CHECK (weekday IS NULL OR (weekday >= 0 AND weekday <= 6)),
    ADD COLUMN day_plan_type text CHECK (day_plan_type IS NULL OR day_plan_type IN ('no_carb', 'low_carb', 'mid_carb', 'high_carb', 'custom')),
    ADD COLUMN day_custom_carb_g double precision CHECK (day_custom_carb_g IS NULL OR day_custom_carb_g >= 0),
    ADD COLUMN day_custom_protein_g double precision CHECK (day_custom_protein_g IS NULL OR day_custom_protein_g >= 0),
    ADD COLUMN day_custom_fat_g double precision CHECK (day_custom_fat_g IS NULL OR day_custom_fat_g >= 0),
    ADD COLUMN overage_confirmed boolean NOT NULL DEFAULT false;

-- Each day's overrides go back onto every one of its meals.
UPDATE meals m SET
    weekday = d.weekday,
    day_plan_type = d.carb_type,
    day_custom_carb_g = d.carb_g,
    day_custom_protein_g = d.protein_g,
    day_custom_fat_g = d.fat_g
FROM meal_plan_days d
WHERE d.id = m.day_id;

ALTER TABLE meals
    DROP CONSTRAINT meals_day_number_key,
    DROP CONSTRAINT meals_day_fk,
    DROP COLUMN day_id;

DROP TABLE meal_plan_days;

CREATE UNIQUE INDEX meals_plan_weekday_number_uidx
    ON meals (meal_plan_id, COALESCE(weekday, -1), meal_number);

-- +goose StatementEnd
