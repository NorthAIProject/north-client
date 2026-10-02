-- +goose Up
-- +goose StatementBegin

-- Decision revisits: the coach asks 30 and 90 days after a call whether it
-- held. held is the answer; held_at is when it was last given, so the 90-day
-- question still comes after a 30-day answer.
ALTER TABLE decisions
    ADD COLUMN held    text CHECK (held IN ('yes', 'partly', 'no')),
    ADD COLUMN held_at timestamptz;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE decisions DROP COLUMN held_at, DROP COLUMN held;
-- +goose StatementEnd
