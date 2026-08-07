-- +goose Up
-- +goose StatementBegin

-- Which AI providers a user is served from. North runs a chain of backends —
-- paid, self-hosted, and free — and the tier decides which chain applies, so a
-- free account can be carried by the free and self-hosted models without
-- spending credit on it.
--
-- A column on users rather than a table: there is one value per account, it is
-- read on every coaching turn, and no history of it is worth keeping. Billing,
-- when it exists, will own the transitions; this only records the current
-- state.
ALTER TABLE users
    ADD COLUMN tier text NOT NULL DEFAULT 'free'
        CHECK (tier IN ('free', 'pro'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN tier;
-- +goose StatementEnd
