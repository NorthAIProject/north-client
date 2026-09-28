-- +goose Up
-- +goose StatementBegin

-- The plan day a guided workout was opened for. A Wednesday session started
-- on Tuesday still belongs to Wednesday. NULL is a log, an import, or a
-- session from before this column: those match the weekday they started on
-- when they are strength training.
ALTER TABLE activity_sessions ADD COLUMN plan_weekday text;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE activity_sessions DROP COLUMN plan_weekday;
-- +goose StatementEnd
