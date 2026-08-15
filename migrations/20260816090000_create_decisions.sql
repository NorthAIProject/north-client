-- +goose Up
-- +goose StatementBegin

-- A decision is a point-in-time call: what was chosen, what was on the table,
-- why, and (later) what happened. Distinct from journal_entries, which are
-- free-form reflections, and from goals, which are standing intentions.
CREATE TABLE decisions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    title       text        NOT NULL,
    options     text        NOT NULL DEFAULT '',
    rationale   text        NOT NULL DEFAULT '',
    -- Filled when the person looks back. Empty until then; the later review
    -- UI writes this column rather than needing a second migration.
    outcome     text        NOT NULL DEFAULT '',

    decided_at  timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX decisions_user_decided_idx ON decisions (user_id, decided_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE decisions;
-- +goose StatementEnd
