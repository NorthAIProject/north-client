-- +goose Up
-- +goose StatementBegin

-- Distance covered, for a session someone logs after the fact: a run is
-- "5 km in 28 minutes" to the person who ran it, and the pace is the number a
-- coach reads. NULL means not recorded, which is every timer session and
-- every Strava import — Strava's own distance stays in strava_activities,
-- in Strava's vocabulary, as 00030 explains.
ALTER TABLE activity_sessions ADD COLUMN distance_m double precision;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE activity_sessions DROP COLUMN distance_m;
-- +goose StatementEnd
