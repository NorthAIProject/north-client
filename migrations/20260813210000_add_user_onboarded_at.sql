-- +goose Up
-- +goose StatementBegin

-- NULL means the web app should show first-run onboarding once.
-- Existing accounts are backfilled so only new signups see the flow.
ALTER TABLE users
    ADD COLUMN onboarded_at timestamptz;

UPDATE users SET onboarded_at = created_at WHERE onboarded_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN onboarded_at;
-- +goose StatementEnd
