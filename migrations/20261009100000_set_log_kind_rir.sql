-- +goose Up
-- +goose StatementBegin

-- What kind of set it was, and how close to failure. A warm-up is logged so
-- the session reads as it happened, but it is left out of volume, records and
-- fatigue: it is preparation, not training. A drop set counts as work.
-- Existing rows are all work sets, which is what they were recorded as.
ALTER TABLE set_logs
    ADD COLUMN kind text NOT NULL DEFAULT 'work' CHECK (kind IN ('warmup', 'work', 'drop')),
    -- Reps in reserve: 0 is to failure. NULL when nobody said.
    ADD COLUMN rir smallint CHECK (rir BETWEEN 0 AND 10);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE set_logs DROP COLUMN rir, DROP COLUMN kind;
-- +goose StatementEnd
