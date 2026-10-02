-- +goose Up
-- +goose StatementBegin

-- A lighter day: on a morning when recovery markers are off their baseline,
-- the person chose to train about 60% of today's sets, or to keep the plan.
-- One row per day they answered; no row means they were not asked or did not
-- answer, and the plan stands.
CREATE TABLE lighter_days (
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day         date NOT NULL,
    choice      text NOT NULL CHECK (choice IN ('lighter', 'keep')),
    decided_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, day)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE lighter_days;
-- +goose StatementEnd
