-- +goose Up
-- +goose StatementBegin

-- Nutrients stored per-100g rather than per fixed serving, so any logged
-- quantity scales cleanly without re-deriving a base value first.
CREATE TABLE ingredients (
    id                      uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Null means a shared/global ingredient anyone can log against; set means
    -- a user-created one, visible only to them.
    user_id                 uuid        REFERENCES users (id) ON DELETE CASCADE,

    name                    text        NOT NULL,
    brand                   text        NOT NULL DEFAULT '',
    category                text        NOT NULL DEFAULT 'other',

    -- Display only ("1 medium egg = 50g"); MacrosFor(quantity) always does
    -- the actual math from the per-100g values below.
    serving_size_grams      double precision NOT NULL DEFAULT 100,

    calories_per_100g       double precision NOT NULL,
    protein_g_per_100g      double precision NOT NULL DEFAULT 0,
    fat_g_per_100g          double precision NOT NULL DEFAULT 0,
    carbs_g_per_100g        double precision NOT NULL DEFAULT 0,
    fiber_g_per_100g        double precision NOT NULL DEFAULT 0,
    sugar_g_per_100g        double precision NOT NULL DEFAULT 0,
    sodium_mg_per_100g      double precision NOT NULL DEFAULT 0,
    potassium_mg_per_100g   double precision NOT NULL DEFAULT 0,
    cholesterol_mg_per_100g double precision NOT NULL DEFAULT 0,

    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ingredients_user_idx ON ingredients (user_id);
-- Simple v1 search: case-insensitive prefix/substring via ILIKE against this
-- index. Good enough at this data size; revisit only if it stops being so.
CREATE INDEX ingredients_name_idx ON ingredients (lower(name));

-- A small reference list, not user-editable, so it is seeded here rather than
-- built as an admin feature.
CREATE TABLE diets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        text NOT NULL UNIQUE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT ''
);

INSERT INTO diets (code, name, description) VALUES
    ('vegan', 'Vegan', 'No animal products.'),
    ('vegetarian', 'Vegetarian', 'No meat or fish.'),
    ('pescatarian', 'Pescatarian', 'Vegetarian plus fish and seafood.'),
    ('keto', 'Keto', 'Very low carb, high fat.'),
    ('paleo', 'Paleo', 'Whole foods, no grains or processed sugar.'),
    ('mediterranean', 'Mediterranean', 'Vegetables, fish, and olive oil focused.'),
    ('gluten_free', 'Gluten-free', 'No wheat, barley, or rye.'),
    ('dairy_free', 'Dairy-free', 'No milk-derived products.'),
    ('low_carb', 'Low-carb', 'Reduced carbohydrate intake.'),
    ('high_protein', 'High-protein', 'Protein-forward intake.');

CREATE TABLE user_diet_preferences (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    diet_id    uuid        NOT NULL REFERENCES diets (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, diet_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_diet_preferences;
DROP TABLE diets;
DROP TABLE ingredients;
-- +goose StatementEnd
