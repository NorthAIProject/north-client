-- +goose Up
-- +goose StatementBegin

-- What a device or Strava measured about a session beyond its length and
-- calories. NULL is unknown, never zero: a session without a heart-rate strap
-- did not have a heart rate of 0, and a gym session did not climb 0 metres
-- so much as nobody measured it.
ALTER TABLE activity_sessions
    ADD COLUMN avg_hr      real             CHECK (avg_hr BETWEEN 20 AND 250),
    ADD COLUMN max_hr      real             CHECK (max_hr BETWEEN 20 AND 250),
    ADD COLUMN elevation_m double precision CHECK (elevation_m >= 0),
    -- Treadmill, indoor ride, pool. NULL when the provider did not say.
    ADD COLUMN indoor      boolean;

ALTER TABLE strava_activities
    ADD COLUMN average_heartrate real,
    ADD COLUMN max_heartrate     real,
    -- Strava's own flag for a trainer or treadmill.
    ADD COLUMN trainer           boolean NOT NULL DEFAULT false;

-- Pull the last 90 days from Strava again on the next sweep, so activities
-- already imported pick up heart rate, climb and the trainer flag. Imports
-- are idempotent and only fill what is missing, so this re-reads rather than
-- duplicates.
UPDATE strava_connections SET last_synced_at = NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE strava_activities
    DROP COLUMN trainer,
    DROP COLUMN max_heartrate,
    DROP COLUMN average_heartrate;
ALTER TABLE activity_sessions
    DROP COLUMN indoor,
    DROP COLUMN elevation_m,
    DROP COLUMN max_hr,
    DROP COLUMN avg_hr;
-- +goose StatementEnd
