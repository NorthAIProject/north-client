-- +goose Up
-- +goose StatementBegin

-- A medication someone takes, as they describe it. North records the schedule
-- it is told and never changes or suggests a dose: dose is free text ("50 mg",
-- "1 puff") because it is theirs to state, not ours to compute with.
CREATE TABLE medications (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    dose         text        NOT NULL DEFAULT '' CHECK (length(dose) <= 40),
    -- "HH:MM" slots in the person's zone, sorted. Empty means as needed:
    -- nothing is scheduled and nothing is reminded.
    times_of_day text[]      NOT NULL DEFAULT '{}',
    -- 0=Sunday .. 6=Saturday, matching Go's time.Weekday and meal_reminders.
    days_of_week smallint[]  NOT NULL DEFAULT '{0,1,2,3,4,5,6}',
    remind       boolean     NOT NULL DEFAULT true,
    notes        text        NOT NULL DEFAULT '',

    -- Stopping keeps the row: the doses logged against it are still history.
    stopped_at   timestamptz,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- One active medication per name, so "log my metformin" has one answer. A
-- stopped one does not block starting it again.
CREATE UNIQUE INDEX medications_user_active_name_idx
    ON medications (user_id, lower(name)) WHERE stopped_at IS NULL;

-- A dose taken or skipped. slot is the scheduled "HH:MM" it answers, or NULL
-- for an as-needed dose. Logging a slot again replaces its status; NULL slots
-- never collide, so repeated as-needed doses each keep their own row.
CREATE TABLE medication_logs (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    medication_id uuid        NOT NULL REFERENCES medications (id) ON DELETE CASCADE,
    log_date      date        NOT NULL,
    slot          text,
    status        text        NOT NULL CHECK (status IN ('taken', 'skipped')),
    logged_at     timestamptz NOT NULL DEFAULT now(),

    UNIQUE (medication_id, log_date, slot)
);

CREATE INDEX medication_logs_user_date_idx ON medication_logs (user_id, log_date DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE medication_logs;
DROP TABLE medications;
-- +goose StatementEnd
