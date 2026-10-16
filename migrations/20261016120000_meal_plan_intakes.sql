-- +goose Up
-- +goose StatementBegin

-- What a person asked for when the coach built them a meal plan: the
-- counterpart of workout_intakes. Kept so the form opens with last time's
-- answers, and so the foods they won't eat reach the coach's context in every
-- conversation, not only the one where they said so.
--
-- One row per request, never updated. The newest row is the person's current
-- answers.
CREATE TABLE meal_plan_intakes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    objective text NOT NULL CHECK (objective IN ('cutting', 'maintenance', 'bulking')),
    days smallint NOT NULL CHECK (days BETWEEN 1 AND 7),
    meals_per_day smallint NOT NULL CHECK (meals_per_day BETWEEN 1 AND 8),
    plan_type text NOT NULL,
    exclusions text[] NOT NULL DEFAULT '{}',
    notes text NOT NULL DEFAULT '' CHECK (char_length(notes) <= 500),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX meal_plan_intakes_user_created_idx ON meal_plan_intakes (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE meal_plan_intakes;
-- +goose StatementEnd
