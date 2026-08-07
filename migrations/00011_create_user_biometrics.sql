-- +goose Up
-- +goose StatementBegin

-- Current + history: a new measurement never overwrites the last one, it
-- retires it. The coach and the calculator only ever want the current row;
-- the history stays for trend-spotting later.
CREATE TABLE user_biometrics (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid          NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- double precision rather than numeric: these are display/calculation
    -- inputs, not money, so plain float64 in Go is the simpler fit.
    weight_kg     double precision NOT NULL CHECK (weight_kg > 0),
    height_cm     double precision NOT NULL CHECK (height_cm > 0),
    date_of_birth date          NOT NULL,
    sex           text          NOT NULL CHECK (sex IN ('male', 'female')),

    is_current    boolean       NOT NULL DEFAULT true,

    created_at    timestamptz   NOT NULL DEFAULT now()
);

-- At most one current row per user.
CREATE UNIQUE INDEX user_biometrics_current_uidx ON user_biometrics (user_id) WHERE is_current;
CREATE INDEX user_biometrics_user_created_idx ON user_biometrics (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_biometrics;
-- +goose StatementEnd
