-- +goose Up
-- +goose StatementBegin

-- The weekdays someone said they can train on, Monday first. Empty when they
-- gave only a count, and the plan picks the days.
ALTER TABLE workout_intakes ADD COLUMN weekdays text[] NOT NULL DEFAULT '{}';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE workout_intakes DROP COLUMN weekdays;
-- +goose StatementEnd
