-- +goose Up
-- +goose StatementBegin

-- Excluded facts stay in the user's list but never reach the coach.
-- Distinct from rejected (extraction was wrong) and from soft-delete.
ALTER TABLE user_memories
    ADD COLUMN excluded boolean NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_memories DROP COLUMN excluded;
-- +goose StatementEnd
