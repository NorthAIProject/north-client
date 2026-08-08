-- +goose Up

-- Documents are the third thing North remembers, after profile facts and
-- conversations. A person's training log, a physio's notes, a programme they
-- were handed — the material a coach would ask to read before advising.
--
-- Postgres is the source of truth. document_chunks and index_runs are derived:
-- dropping them loses nothing, and a reindex rebuilds them from the documents
-- and their stored bytes. That is deliberate, and it is what stops the index
-- becoming a second place where the truth lives.

CREATE TABLE documents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    title       text NOT NULL,

    -- 'upload' keeps its bytes in object storage; 'note' is written in North
    -- and lives in this table. Both chunk and rank identically, so nothing
    -- downstream has to care which it is.
    source_kind text NOT NULL CHECK (source_kind IN ('upload', 'note')),

    -- Object storage key for an upload, null for a note. Unique because two
    -- documents pointing at the same bytes would make deletion ambiguous.
    storage_key text UNIQUE,

    -- Body text for a note. Null for an upload, whose text is read from
    -- storage at index time and never duplicated here.
    body        text,

    mime        text NOT NULL,
    byte_size   bigint NOT NULL,

    -- Fingerprint of the text that was last chunked. An unchanged document
    -- reindexes without touching a single chunk row.
    content_sha256 text NOT NULL DEFAULT '',

    line_count  integer NOT NULL DEFAULT 0,

    status      text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'ready', 'failed')),

    -- Why a document failed, in words its owner can act on. Empty otherwise.
    parse_error text NOT NULL DEFAULT '',

    -- Null until first indexed. Older than updated_at means stale — the
    -- document has changed since the coach last had a usable view of it.
    indexed_at  timestamptz,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,

    -- An upload without a key, or a note without a body, would index to
    -- nothing and report success. Refuse it at the boundary instead.
    CONSTRAINT documents_have_their_content CHECK (
        (source_kind = 'upload' AND storage_key IS NOT NULL)
     OR (source_kind = 'note'   AND body IS NOT NULL)
    )
);

CREATE INDEX idx_documents_user ON documents (user_id, updated_at DESC)
    WHERE deleted_at IS NULL;

-- Chunks are the unit of retrieval, and the reason a retrieved fact can be
-- pointed at rather than merely quoted.
--
-- start_line and end_line are one-based and inclusive, over the document's own
-- text. They are the whole point: a citation that cannot be resolved back to a
-- place in the source is not a citation.
CREATE TABLE document_chunks (
    -- Derived from the document, the chunk's position, and a hash of its
    -- content, so re-chunking unchanged text produces the identical id. That
    -- is what makes reindex idempotent and keeps a chunk id quoted in a stored
    -- reply resolvable months later. See internal/documents/ids.go.
    chunk_id    text PRIMARY KEY,

    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,

    -- Denormalised from documents: every retrieval filters on it, and a join
    -- to reach the owner of a row is a join on the hot path of every reply.
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    ordinal     integer NOT NULL,

    -- The heading trail above this chunk, outermost first, as a JSON array.
    -- Carried so a citation can read "Training log › Deload weeks" rather than
    -- "chunk 47".
    heading_path jsonb NOT NULL DEFAULT '[]',

    start_line  integer NOT NULL CHECK (start_line >= 1),
    end_line    integer NOT NULL CHECK (end_line >= start_line),

    -- Named content, not text, so that to_tsvector('english', content) is the
    -- identical expression used on user_memories. One expression across every
    -- searchable table is what internal/search/rank.go asserts.
    content     text NOT NULL,

    content_sha256 text NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (document_id, ordinal)
);

CREATE INDEX idx_document_chunks_search
    ON document_chunks
    USING GIN (to_tsvector('english', content));

CREATE INDEX idx_document_chunks_user ON document_chunks (user_id);
CREATE INDEX idx_document_chunks_document ON document_chunks (document_id, ordinal);

-- What happened the last time indexing ran.
--
-- Without this, "indexing is broken" is the most anyone can say. With it, the
-- answer is which documents failed and why — and a person can be shown that
-- their file was rejected rather than left wondering why the coach has never
-- mentioned it.
CREATE TABLE index_runs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- 'document' for a single file, 'user' for a full rebuild.
    kind        text NOT NULL CHECK (kind IN ('document', 'user')),

    started_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,

    documents_seen      integer NOT NULL DEFAULT 0,
    documents_added     integer NOT NULL DEFAULT 0,
    documents_updated   integer NOT NULL DEFAULT 0,
    documents_unchanged integer NOT NULL DEFAULT 0,
    documents_failed    integer NOT NULL DEFAULT 0,
    chunks_written      integer NOT NULL DEFAULT 0,
    chunks_removed      integer NOT NULL DEFAULT 0,

    -- Per-document problems that did not fail the run: an empty file, a
    -- document whose bytes had gone from storage.
    warnings    jsonb NOT NULL DEFAULT '[]',

    success       boolean NOT NULL DEFAULT false,
    error_summary text NOT NULL DEFAULT ''
);

CREATE INDEX idx_index_runs_user ON index_runs (user_id, started_at DESC);

-- +goose Down

DROP TABLE index_runs;
DROP TABLE document_chunks;
DROP TABLE documents;
