-- +goose Up
-- +goose StatementBegin

-- The plan someone follows. Until now that was whichever plan row was written
-- last, so editing an old plan to look at it quietly made it the one followed.
-- A plan is an intake (see ListCurrentPlans); its newest version is read.
CREATE TABLE workout_active_plans (
    user_id    uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    intake_id  uuid NOT NULL REFERENCES workout_intakes(id) ON DELETE CASCADE,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Everyone keeps following what they follow today.
INSERT INTO workout_active_plans (user_id, intake_id)
SELECT DISTINCT ON (user_id) user_id, intake_id
FROM workout_plans
ORDER BY user_id, created_at DESC;

-- What a week trains. week_start is its Monday in the person's own time zone.
-- slots is [{weekday, intake_id, day_index}]: on that weekday, the day at
-- day_index of that plan. A week is written the first time it is read, so the
-- next week knows where the rotation stopped, and again whenever someone
-- changes it; custom marks the second kind.
CREATE TABLE workout_weeks (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start date NOT NULL,
    slots      jsonb NOT NULL DEFAULT '[]',
    custom     boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, week_start)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE workout_weeks;
DROP TABLE workout_active_plans;
-- +goose StatementEnd
