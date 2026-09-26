-- +goose Up
-- +goose StatementBegin

-- One working set: what was lifted and how many times. Append-per-event, like
-- caffeine_logs; a workout is the sets that share an activity session, or,
-- for sets logged without one, the local day.
CREATE TABLE set_logs (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    activity_session_id uuid        REFERENCES activity_sessions (id) ON DELETE SET NULL,
    log_date            date        NOT NULL,

    -- The catalog slug when the exercise came from the catalog, '' for one
    -- typed in; exercise_name is always what the person saw.
    exercise_slug       text        NOT NULL DEFAULT '',
    exercise_name       text        NOT NULL CHECK (length(exercise_name) BETWEEN 1 AND 120),
    set_number          integer     NOT NULL CHECK (set_number BETWEEN 1 AND 50),

    -- 0 is bodyweight. The bounds are typo guards, not physiology.
    weight_kg           double precision NOT NULL CHECK (weight_kg >= 0 AND weight_kg <= 600),
    reps                integer     NOT NULL CHECK (reps BETWEEN 1 AND 100),

    performed_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX set_logs_user_performed_idx ON set_logs (user_id, performed_at DESC);
CREATE INDEX set_logs_user_exercise_idx ON set_logs (user_id, lower(exercise_name), performed_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE set_logs;
-- +goose StatementEnd
