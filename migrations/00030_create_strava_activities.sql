-- +goose Up
-- +goose StatementBegin

-- The provider's own view of an activity, kept alongside the normalised
-- session it produced in activity_sessions.
--
-- Two tables rather than more columns on activity_sessions, because these are
-- different things: activity_sessions is North's vocabulary (a MET code, a
-- calorie figure, a duration) and must stay identical whether a session was
-- tracked in-app or imported. This is Strava's vocabulary — routes, elevation,
-- their own naming — and it exists so the 3D view has something to draw
-- without a round trip to Strava on every page load.
--
-- Numbered 00030 rather than 00023, which is the next free number on main:
-- the fitme-port branch has claimed 00023 through 00026 without merging, and
-- has applied them to the shared development database. The gap is deliberate
-- headroom. Sequential numbering does not survive parallel branches; a
-- timestamp-based scheme would, and is worth considering.
CREATE TABLE strava_activities (
    id                     uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- Strava's activity id. Unique per user rather than globally: two North
    -- accounts could legitimately connect the same Strava athlete.
    strava_id              bigint      NOT NULL,

    name                   text        NOT NULL DEFAULT '',
    sport_type             text        NOT NULL DEFAULT '',

    start_date             timestamptz NOT NULL,

    distance_m             double precision NOT NULL DEFAULT 0,
    moving_time_s          integer     NOT NULL DEFAULT 0,
    elapsed_time_s         integer     NOT NULL DEFAULT 0,
    total_elevation_gain_m double precision NOT NULL DEFAULT 0,
    average_speed_ms       double precision NOT NULL DEFAULT 0,

    -- Google-encoded polyline of the route, empty for anything without GPS
    -- (a treadmill run, a gym session). The viewer draws a route when this is
    -- present and falls back to a plain marker when it is not, so indoor work
    -- still appears rather than silently vanishing.
    summary_polyline       text        NOT NULL DEFAULT '',

    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, strava_id)
);

CREATE INDEX strava_activities_user_start_idx ON strava_activities (user_id, start_date DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE strava_activities;
-- +goose StatementEnd
