-- +goose Up
-- +goose StatementBegin

-- Whether a person allows their data to go to third-party AI providers. No row
-- means they have not been asked, which refuses like a decline does. Declining
-- is stored rather than deleted so nobody is asked again after answering.
CREATE TABLE ai_consents (
    user_id    uuid        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    status     text        NOT NULL CHECK (status IN ('granted', 'declined')),
    -- The wording they answered, so a later change of recipients can ask again.
    version    integer     NOT NULL,
    decided_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ai_consents;
-- +goose StatementEnd
