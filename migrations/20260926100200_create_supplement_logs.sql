-- +goose Up
-- +goose StatementBegin

-- A supplement taken: append-per-event. nutrients lists which of the tracked
-- micronutrients it covers (internal/supplements/supplement), copied at write
-- time so an entry still reads the same if the catalogue changes.
CREATE TABLE supplement_logs (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    log_date  date        NOT NULL,

    name      text        NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    count     smallint    NOT NULL DEFAULT 1 CHECK (count BETWEEN 1 AND 20),
    nutrients text[]      NOT NULL DEFAULT '{}',

    logged_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX supplement_logs_user_logged_idx ON supplement_logs (user_id, logged_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE supplement_logs;
-- +goose StatementEnd
