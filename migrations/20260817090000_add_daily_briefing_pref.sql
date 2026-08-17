-- +goose Up
-- +goose StatementBegin

-- Opt-in for the morning briefing.
--
-- Off by default for the same reason weekly_report_auto is (00xxx): generating
-- one is a model call nobody asked for, and this one would recur every single
-- morning rather than once a week.
ALTER TABLE user_notification_prefs
    ADD COLUMN daily_briefing_auto boolean NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_notification_prefs
    DROP COLUMN daily_briefing_auto;
-- +goose StatementEnd
