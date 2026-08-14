-- +goose Up

CREATE TABLE vault_connections (
    user_id      uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    root_path    text        NOT NULL,
    last_sync_at timestamptz,
    last_error   text        NOT NULL DEFAULT '',
    enabled      boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE documents DROP CONSTRAINT documents_source_kind_check;
ALTER TABLE documents ADD CONSTRAINT documents_source_kind_check
    CHECK (source_kind IN ('upload', 'note', 'vault'));

ALTER TABLE documents DROP CONSTRAINT documents_have_their_content;
ALTER TABLE documents ADD CONSTRAINT documents_have_their_content CHECK (
    (source_kind = 'upload' AND storage_key IS NOT NULL)
 OR (source_kind = 'note'   AND body IS NOT NULL)
 OR (source_kind = 'vault'  AND storage_key IS NOT NULL)
);

ALTER TABLE documents
    ADD COLUMN external_path text,
    ADD COLUMN external_mtime timestamptz;

CREATE UNIQUE INDEX documents_vault_path_idx
    ON documents (user_id, external_path)
    WHERE source_kind = 'vault' AND deleted_at IS NULL AND external_path IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS documents_vault_path_idx;

ALTER TABLE documents
    DROP COLUMN IF EXISTS external_mtime,
    DROP COLUMN IF EXISTS external_path;

ALTER TABLE documents DROP CONSTRAINT documents_have_their_content;
ALTER TABLE documents ADD CONSTRAINT documents_have_their_content CHECK (
    (source_kind = 'upload' AND storage_key IS NOT NULL)
 OR (source_kind = 'note'   AND body IS NOT NULL)
);

ALTER TABLE documents DROP CONSTRAINT documents_source_kind_check;
ALTER TABLE documents ADD CONSTRAINT documents_source_kind_check
    CHECK (source_kind IN ('upload', 'note'));

DROP TABLE vault_connections;
