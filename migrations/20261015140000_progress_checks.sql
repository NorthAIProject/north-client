-- +goose Up
-- +goose StatementBegin

-- A progress check: the weight and a few circumferences a trainer would take,
-- with photos as evidence, on a date. Every measurement is optional except as
-- the figure needs it; the photos never drive a number on their own. Metric
-- only: cm and kg, converted for display.
CREATE TABLE progress_checks (
    id                uuid             PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid             NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    measured_on       date             NOT NULL,

    -- The scale reading, or the last known weight when none was given; then
    -- weight_carried is true so a trend never reads it as a fresh reading.
    weight_kg         double precision CHECK (weight_kg BETWEEN 20 AND 400),
    weight_carried    boolean          NOT NULL DEFAULT false,

    waist_cm          double precision CHECK (waist_cm BETWEEN 40 AND 250),
    hips_cm           double precision CHECK (hips_cm BETWEEN 50 AND 250),
    chest_cm          double precision CHECK (chest_cm BETWEEN 50 AND 250),
    thigh_cm          double precision CHECK (thigh_cm BETWEEN 25 AND 120),
    arm_cm            double precision CHECK (arm_cm BETWEEN 15 AND 80),

    -- manual: typed by the person. assisted: a model's estimate the person
    -- accepted or edited. Either way the person confirmed it before saving.
    source            text             NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'assisted')),
    assist_confidence text             CHECK (assist_confidence IN ('low', 'medium', 'high')),
    assist_notes      text             NOT NULL DEFAULT '' CHECK (length(assist_notes) <= 280),

    created_at        timestamptz      NOT NULL DEFAULT now()
);

CREATE INDEX progress_checks_user_measured_idx ON progress_checks (user_id, measured_on DESC, created_at DESC);

-- One photo per pose per check. The media row owns the object in storage;
-- deleting a check deletes its media through the service, which removes the
-- objects too, so this cascade only ever tidies what is already gone.
CREATE TABLE progress_check_photos (
    check_id uuid NOT NULL REFERENCES progress_checks (id) ON DELETE CASCADE,
    media_id uuid NOT NULL REFERENCES media (id) ON DELETE CASCADE,
    pose     text NOT NULL CHECK (pose IN ('front', 'side', 'back')),

    PRIMARY KEY (check_id, pose)
);

-- The cadence a person chose, and when the next check falls due. Saving a
-- check moves next_due_on on from its date; skipping a window leaves it due.
CREATE TABLE progress_check_schedules (
    user_id     uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    cadence     text        NOT NULL CHECK (cadence IN ('weekly', 'biweekly', 'monthly')),
    next_due_on date        NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE progress_check_schedules;
DROP TABLE progress_check_photos;
DROP TABLE progress_checks;
-- +goose StatementEnd
