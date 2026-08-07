-- +goose Up
-- +goose StatementBegin

-- Each generated plan snapshots the inputs it was computed from, so a plan
-- explains itself later without joining back through biometrics history that
-- may have since moved on.
CREATE TABLE user_macro_plans (
    id             uuid          PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid          NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- double precision throughout: these are calculator outputs, not money.
    weight_kg      double precision NOT NULL,
    height_cm      double precision NOT NULL,
    age            smallint      NOT NULL,
    sex            text          NOT NULL CHECK (sex IN ('male', 'female')),

    activity_level text          NOT NULL CHECK (activity_level IN ('sedentary', 'light', 'moderate', 'heavy', 'extra_heavy')),
    goal           text          NOT NULL CHECK (goal IN ('cutting', 'maintenance', 'bulking')),
    macro_split    text          NOT NULL CHECK (macro_split IN ('high_carb', 'moderate_carb', 'low_carb')),

    bmr            double precision NOT NULL,
    tdee           double precision NOT NULL,
    calorie_goal   double precision NOT NULL,
    protein_g      double precision NOT NULL,
    fat_g          double precision NOT NULL,
    carb_g         double precision NOT NULL,

    is_current     boolean       NOT NULL DEFAULT true,

    created_at     timestamptz   NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX user_macro_plans_current_uidx ON user_macro_plans (user_id) WHERE is_current;
CREATE INDEX user_macro_plans_user_created_idx ON user_macro_plans (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_macro_plans;
-- +goose StatementEnd
