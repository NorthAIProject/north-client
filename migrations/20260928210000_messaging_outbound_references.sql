-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS messaging_outbound_references (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform     text        NOT NULL,
    external_id  text        NOT NULL,
    message_id   bigint      NOT NULL,
    kind         text        NOT NULL,
    dedupe_key   text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS messaging_outbound_ref_idx
    ON messaging_outbound_references (user_id, platform, kind, dedupe_key);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS messaging_outbound_references;
-- +goose StatementEnd
