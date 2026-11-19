-- +goose Up

-- A forecast as it was made, kept so it can later be compared with what
-- happened. One row per person, week, series and horizon: the weekly job
-- writes them, and nothing ever rewrites one, or the record would be of
-- forecasts improved after the fact.
CREATE TABLE horizon_snapshots (
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The person's own calendar date the forecast was made on.
    made_on       date NOT NULL,
    -- "lift:<key>", "weight" or "waist".
    series_key    text NOT NULL,
    horizon_weeks integer NOT NULL CHECK (horizon_weeks > 0),
    low           double precision NOT NULL,
    mid           double precision NOT NULL,
    high          double precision NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, series_key, horizon_weeks, made_on),
    CHECK (low <= mid AND mid <= high)
);

-- +goose Down
DROP TABLE horizon_snapshots;
