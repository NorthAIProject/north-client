-- +goose Up
-- +goose StatementBegin

CREATE TABLE food_logs (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    log_date       date        NOT NULL,

    -- Exactly one of these two is set: a logged meal-plan meal, or an
    -- ad-hoc ingredient + quantity.
    meal_id        uuid        REFERENCES meals (id) ON DELETE SET NULL,
    ingredient_id  uuid        REFERENCES ingredients (id) ON DELETE SET NULL,
    quantity_grams double precision,

    -- Denormalized so the entry still reads sensibly if its source is later
    -- deleted (meal/ingredient FKs above go to NULL, this does not).
    label          text        NOT NULL,

    -- Snapshot of macros at log time, same reasoning as meal_ingredients.
    calories       double precision NOT NULL,
    protein_g      double precision NOT NULL,
    fat_g          double precision NOT NULL,
    carbs_g        double precision NOT NULL,

    logged_at      timestamptz NOT NULL DEFAULT now(),

    CHECK (num_nonnulls(meal_id, ingredient_id) = 1)
);

CREATE INDEX food_logs_user_date_idx ON food_logs (user_id, log_date DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE food_logs;
-- +goose StatementEnd
