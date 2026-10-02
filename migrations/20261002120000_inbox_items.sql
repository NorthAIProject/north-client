-- +goose Up
-- +goose StatementBegin

-- The capture inbox: anything saved from the share sheet, a shortcut or the
-- app without deciding where it belongs. The coach suggests a home; the
-- person confirms. Nothing is filed without them.
CREATE TABLE inbox_items (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source      text NOT NULL CHECK (source IN ('app', 'share', 'shortcut', 'web')),
    text        text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 4000),
    -- The coach's suggestion, once made: {destination, goal_id, title, why}.
    suggestion  jsonb,
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'filed', 'dismissed')),
    -- Where it went when filed: goal_note, knowledge or journal, and the id
    -- of what was written there.
    filed_as    text CHECK (filed_as IN ('goal_note', 'knowledge', 'journal')),
    filed_ref   uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX inbox_items_open ON inbox_items (user_id, created_at DESC) WHERE status = 'open';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE inbox_items;
-- +goose StatementEnd
