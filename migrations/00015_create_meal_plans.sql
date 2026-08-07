-- +goose Up
-- +goose StatementBegin

CREATE TABLE meal_plans (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    name           text        NOT NULL,
    description    text        NOT NULL DEFAULT '',
    objective      text        NOT NULL DEFAULT '',
    activity_level text        NOT NULL DEFAULT '',
    gender         text        NOT NULL DEFAULT '',

    -- Denormalized cache of the sum of this plan's meals, kept current by
    -- the service on every ingredient add/remove rather than re-summed on
    -- every read.
    total_macros   jsonb       NOT NULL DEFAULT '{}',

    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX meal_plans_user_idx ON meal_plans (user_id, created_at DESC);

CREATE TABLE meals (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    meal_plan_id uuid        NOT NULL REFERENCES meal_plans (id) ON DELETE CASCADE,

    meal_number  smallint    NOT NULL,
    name         text        NOT NULL,

    total_macros jsonb       NOT NULL DEFAULT '{}',

    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (meal_plan_id, meal_number)
);

CREATE INDEX meals_plan_idx ON meals (meal_plan_id, meal_number);

CREATE TABLE meal_ingredients (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    meal_id        uuid        NOT NULL REFERENCES meals (id) ON DELETE CASCADE,

    -- RESTRICT rather than SET NULL: a meal ingredient with no ingredient
    -- would show as a blank line with real macros attached to nothing.
    -- Deleting an in-use ingredient should fail loudly instead.
    ingredient_id  uuid        NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,

    quantity_grams double precision NOT NULL CHECK (quantity_grams > 0),

    -- Snapshot of the macros for this quantity at insert time, so editing the
    -- underlying ingredient later never silently rewrites a meal's history.
    calories       double precision NOT NULL,
    protein_g      double precision NOT NULL,
    fat_g          double precision NOT NULL,
    carbs_g        double precision NOT NULL,

    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX meal_ingredients_meal_idx ON meal_ingredients (meal_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE meal_ingredients;
DROP TABLE meals;
DROP TABLE meal_plans;
-- +goose StatementEnd
