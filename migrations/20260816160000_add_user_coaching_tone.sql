-- +goose Up
-- +goose StatementBegin

-- The promptable half of "how you want to be coached".
--
-- coaching_style (00001) stays free text on purpose: it is where somebody
-- says something no enum could hold. But free text alone gives the prompt
-- builder nothing stable to render, nothing to test, and a brand-new account
-- nothing at all. A tone is a small, closed set, so the coach always has a
-- voice even before anyone opens settings.
--
-- text + CHECK rather than a Postgres enum: adding a tone later is one
-- migration that rewrites this constraint, not an ALTER TYPE that cannot be
-- rolled back inside a transaction.
ALTER TABLE users
    ADD COLUMN coaching_tone text NOT NULL DEFAULT 'direct'
        CHECK (coaching_tone IN ('direct', 'warm', 'analytical', 'tough_love'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN coaching_tone;
-- +goose StatementEnd
