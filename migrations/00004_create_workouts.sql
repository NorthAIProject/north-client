-- +goose Up
-- +goose StatementBegin

CREATE TABLE workout_intakes (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    goal             text        NOT NULL,
    experience       text        NOT NULL,

    -- The constraints a generated plan is validated against. Stored rather than
    -- only sent to the model, so a plan can be re-checked later and so the next
    -- intake can be pre-filled with what the user already told us.
    days_per_week    smallint    NOT NULL CHECK (days_per_week BETWEEN 1 AND 7),
    session_minutes  smallint    NOT NULL CHECK (session_minutes BETWEEN 10 AND 240),
    equipment        text[]      NOT NULL DEFAULT '{}',
    limitations      text        NOT NULL DEFAULT '',

    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX workout_intakes_user_created_idx ON workout_intakes (user_id, created_at DESC);

CREATE TABLE workout_plans (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    intake_id  uuid        NOT NULL REFERENCES workout_intakes (id) ON DELETE CASCADE,

    name       text        NOT NULL,

    -- The plan body as returned by the model, already validated against the
    -- intake. jsonb rather than a table per exercise: nothing queries inside a
    -- plan yet, and inventing five tables before there is a query to serve is
    -- the kind of premature structure CLAUDE.md warns about.
    plan       jsonb       NOT NULL,

    model      text        NOT NULL,
    provider   text        NOT NULL,

    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX workout_plans_user_created_idx ON workout_plans (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE workout_plans;
DROP TABLE workout_intakes;
-- +goose StatementEnd
