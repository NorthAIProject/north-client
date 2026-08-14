-- +goose Up
-- +goose StatementBegin

-- Per-user gateway address for backends that have no public default
-- (Hermes). Empty for catalogue providers whose URL is fixed.
ALTER TABLE user_ai_credentials
    ADD COLUMN base_url text NOT NULL DEFAULT '' CHECK (length(base_url) <= 500);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_ai_credentials DROP COLUMN base_url;
-- +goose StatementEnd
