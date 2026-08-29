-- +goose Up
-- +goose StatementBegin

-- Which bot account a delivery watermark belongs to.
--
-- last_update_id exists to recognise a redelivery: an adapter acknowledges an
-- update before it answers it, so the platform resends anything whose
-- acknowledgement was lost, and the watermark is what stops the second copy
-- being answered twice.
--
-- The flaw is that a Telegram update id is a counter *per bot*, not a global
-- one. Point the same deployment at a different bot — a rebrand, a rotated
-- project, a test bot swapped for a real one — and the new bot's sequence
-- starts from its own low number. Every update then compares as older than the
-- watermark the previous bot left behind, so every message is classified as a
-- redelivery and dropped.
--
-- The failure is silent and permanent. It logs at INFO, answers nothing, and
-- never recovers on its own, because the watermark can only go up. It cost a
-- live test to find: two real messages were discarded with the bot looking
-- healthy in every other respect.
--
-- Recording which account produced a watermark makes a bot change a new
-- sequence rather than an old one running backwards.
ALTER TABLE messaging_links
    ADD COLUMN account_id text NOT NULL DEFAULT '';

COMMENT ON COLUMN messaging_links.account_id IS
    'The platform account (Telegram bot id) whose sequence last_update_id belongs to. Empty when the platform has no such notion.';

-- Existing rows keep '' and therefore keep the old behaviour until the adapter
-- claims one, at which point the account is recorded and the watermark resets.
-- No backfill: '' is honest about not knowing which bot wrote those rows.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE messaging_links DROP COLUMN IF EXISTS account_id;
-- +goose StatementEnd
