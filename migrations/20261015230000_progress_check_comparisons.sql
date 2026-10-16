-- +goose Up
-- +goose StatementBegin

-- A model's observation of what changed between a person's first progress
-- check with photos and their latest one. One per person: comparing again
-- replaces it. Text only; the photos stay in progress_check_photos.
--
-- Both checks cascade: the observation is derived from their photos, so it
-- must not outlive either of them.
CREATE TABLE progress_check_comparisons (
    user_id        uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    from_check_id  uuid        NOT NULL REFERENCES progress_checks (id) ON DELETE CASCADE,
    to_check_id    uuid        NOT NULL REFERENCES progress_checks (id) ON DELETE CASCADE,
    summary        text        NOT NULL CHECK (length(summary) BETWEEN 1 AND 600),
    confidence     text        NOT NULL CHECK (confidence IN ('low', 'medium', 'high')),
    limits         text        NOT NULL DEFAULT '' CHECK (length(limits) <= 280),
    -- The provider that read the photos, as the person was told before sending.
    provider_label text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CHECK (from_check_id <> to_check_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE progress_check_comparisons;
-- +goose StatementEnd
