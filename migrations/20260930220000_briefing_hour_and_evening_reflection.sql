-- +goose Up
-- +goose StatementBegin

-- When the morning briefing arrives, and the evening reflection.
--
-- briefing_hour replaces a constant 05:00. A push at five in the morning
-- is read at seven at best, and by then it is two hours stale; seven is the
-- default because it is when most people pick the phone up. It only matters
-- to accounts with daily_briefing_auto on.
--
-- evening_reflection is off by default for the reason every other unasked-for
-- message is: turning it on for somebody is sending them a notification every
-- night they did not ask for.
ALTER TABLE user_notification_prefs
    ADD COLUMN briefing_hour smallint NOT NULL DEFAULT 7
        CHECK (briefing_hour BETWEEN 0 AND 23),
    ADD COLUMN evening_reflection boolean NOT NULL DEFAULT false,
    ADD COLUMN evening_hour smallint NOT NULL DEFAULT 21
        CHECK (evening_hour BETWEEN 0 AND 23);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_notification_prefs
    DROP COLUMN evening_hour,
    DROP COLUMN evening_reflection,
    DROP COLUMN briefing_hour;
-- +goose StatementEnd
