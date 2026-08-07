-- +goose Up
-- +goose StatementBegin

-- A live setting, not a point-in-time measurement (unlike biometrics/macro
-- plans), so one row per user updated in place is the right level of
-- complexity — no history needed for "what units does this person use."
CREATE TABLE user_preferences (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,

    units_system        text        NOT NULL DEFAULT 'metric' CHECK (units_system IN ('metric', 'imperial')),
    default_goal        text        NOT NULL DEFAULT 'maintenance' CHECK (default_goal IN ('cutting', 'maintenance', 'bulking')),
    default_macro_split text        NOT NULL DEFAULT 'moderate_carb' CHECK (default_macro_split IN ('high_carb', 'moderate_carb', 'low_carb')),

    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE user_preferences;
-- +goose StatementEnd
