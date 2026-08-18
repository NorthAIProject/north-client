-- +goose Up
-- +goose StatementBegin

-- Kinds are validated in Go. A CHECK here meant every new note required a
-- migration, which is how first-week and training reminders would have been
-- blocked on a schema change.
ALTER TABLE user_nudges DROP CONSTRAINT IF EXISTS user_nudges_kind_check;

ALTER TABLE user_notification_prefs
    ADD COLUMN IF NOT EXISTS coach_activity boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS training_reminders boolean NOT NULL DEFAULT true;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_notification_prefs
    DROP COLUMN IF EXISTS coach_activity,
    DROP COLUMN IF EXISTS training_reminders;

ALTER TABLE user_nudges
    ADD CONSTRAINT user_nudges_kind_check
    CHECK (kind IN ('missed_checkin', 'goal_deadline'));
-- +goose StatementEnd
