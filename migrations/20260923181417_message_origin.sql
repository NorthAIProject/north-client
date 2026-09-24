-- +goose Up
-- +goose StatementBegin

-- Whether a turn answered something the person said, or arrived on its own:
-- the morning briefing, a standing task running on its schedule. The chat
-- draws a caption above a proactive bubble so nobody wonders what they asked
-- to get it (_reviews/muse-chat-contract.md, "Proactive messages").
--
-- Additive with defaults, so every existing row is a reply with no label,
-- which is exactly what every existing row was.
ALTER TABLE messages
    ADD COLUMN origin text NOT NULL DEFAULT 'reply'
        CHECK (origin IN ('reply', 'proactive')),
    -- What sent a proactive turn, in the product's own words: "Daily
    -- briefing", "Standing task". Empty on a reply. Stored as the label rather
    -- than a code so a new source is a line of Go, not a migration; the page
    -- maps the labels it knows onto translated captions.
    ADD COLUMN source_label text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE messages DROP COLUMN IF EXISTS source_label;
ALTER TABLE messages DROP COLUMN IF EXISTS origin;
-- +goose StatementEnd
