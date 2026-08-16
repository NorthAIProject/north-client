-- +goose Up
-- +goose StatementBegin

-- What North is allowed to say without being asked.
--
-- A table of its own rather than columns on users or user_preferences: both
-- of those are written by a single form that replaces the whole row, so
-- folding these in would mean saving fitness defaults silently resets
-- somebody's quiet hours.
--
-- Defaults are chosen so that applying this migration changes nothing for
-- anyone who already has an account: the two nudge kinds are on, because
-- that is what the sweep does today, and the weekly review stays opt-in
-- because generating one costs a model call nobody asked for.
CREATE TABLE user_notification_prefs (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              uuid        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,

    nudge_missed_checkin boolean     NOT NULL DEFAULT true,
    nudge_goal_deadline  boolean     NOT NULL DEFAULT true,

    -- Off by default; see above.
    weekly_report_auto   boolean     NOT NULL DEFAULT false,

    -- Hours in which North stays quiet, read in the user's timezone. The
    -- window may wrap midnight (22:00 -> 07:00), which is why it is two
    -- points rather than a start plus a duration. "HH:MM" text for the same
    -- reason meal_reminders.time_of_day is text (00017): a comparison
    -- string, not a value needing interval arithmetic.
    quiet_hours_enabled  boolean     NOT NULL DEFAULT false,
    quiet_start          text        NOT NULL DEFAULT '22:00' CHECK (quiet_start ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    quiet_end            text        NOT NULL DEFAULT '07:00' CHECK (quiet_end ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),

    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_notification_prefs;
-- +goose StatementEnd
