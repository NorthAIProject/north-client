-- +goose Up
-- +goose StatementBegin

-- Drop the CHECK that limited activity_sessions.source to ('manual','strava').
--
-- It was written when Strava was the only sync, and it has since become the
-- thing standing between a new provider and its first row: every provider —
-- a phone pushing workouts, a Whoop sync, a Garmin import — needed a migration
-- before it could write anything at all. A constraint whose only effect is to
-- require a schema change per feature is not protecting the data.
--
-- What replaces it is not nothing. The (source, external_id) unique index still
-- makes imports idempotent, and the source strings callers may use are bounded
-- at the edge, where a person can actually be told what went wrong, rather than
-- by a constraint violation surfacing as a 500. health_metrics.source was left
-- unconstrained from the start for the same reason, and this brings the older
-- table into line with it.
ALTER TABLE activity_sessions DROP CONSTRAINT IF EXISTS activity_sessions_source_check;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restoring the constraint would fail on any row a provider added while it was
-- absent, so rows outside the original two sources are moved to 'manual'
-- first. That loses which device recorded them, which is the honest cost of
-- going back and the reason this direction is not routine.
UPDATE activity_sessions SET source = 'manual' WHERE source NOT IN ('manual', 'strava');

ALTER TABLE activity_sessions
    ADD CONSTRAINT activity_sessions_source_check CHECK (source IN ('manual', 'strava'));

-- +goose StatementEnd
