-- +goose Up
-- +goose StatementBegin

-- A new person's first seven days: one goal, one small daily action, and the
-- coach running the week. One row per person; starting again replaces it.
-- The day number is never stored: it is the count of local days since
-- started_on in the timezone captured at the start, so it cannot drift.
CREATE TABLE first_week_programmes (
    user_id     uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    goal_id     uuid        REFERENCES goals (id) ON DELETE SET NULL,
    -- sleep, train, protein, journal or custom. Text rather than an enum so a
    -- new template is a code change, not a migration.
    template    text        NOT NULL,
    -- The IANA zone the week runs in, fixed at the start: a person flying
    -- mid-week must not skip or repeat a day.
    timezone    text        NOT NULL,
    -- Day 0, the local date onboarding finished.
    started_on  date        NOT NULL,
    status      text        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'finished', 'skipped', 'abandoned')),
    -- The day-7 review, once written.
    review      jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

-- The sweep visits only the weeks still running.
CREATE INDEX first_week_programmes_active_idx ON first_week_programmes (started_on)
    WHERE status = 'active';

-- Each programme day the action was done. The primary key makes completion
-- happen once, which is what lets first_week_day_completed fire once.
CREATE TABLE first_week_days (
    user_id      uuid        NOT NULL REFERENCES first_week_programmes (user_id) ON DELETE CASCADE,
    day          smallint    NOT NULL CHECK (day BETWEEN 1 AND 7),
    local_date   date        NOT NULL,
    -- log: the matching entry was found; tap: the person said it was done.
    via          text        NOT NULL CHECK (via IN ('log', 'tap')),
    completed_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, day)
);

-- The morning message of the first week is the day's one unprompted coach
-- message, so it shares the once-a-day row with the other triggers.
ALTER TABLE coach_outreach DROP CONSTRAINT coach_outreach_trigger_check;
ALTER TABLE coach_outreach ADD CONSTRAINT coach_outreach_trigger_check
    CHECK (trigger IN ('low_recovery', 'missed_session', 'first_week'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM coach_outreach WHERE trigger = 'first_week';
ALTER TABLE coach_outreach DROP CONSTRAINT coach_outreach_trigger_check;
ALTER TABLE coach_outreach ADD CONSTRAINT coach_outreach_trigger_check
    CHECK (trigger IN ('low_recovery', 'missed_session'));
DROP TABLE first_week_days;
DROP TABLE first_week_programmes;
-- +goose StatementEnd
