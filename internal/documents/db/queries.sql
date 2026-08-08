-- name: CreateDocument :one
INSERT INTO documents (
    user_id, title, source_kind, storage_key, body, mime, byte_size
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetDocument :one
SELECT * FROM documents
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: ListDocuments :many
SELECT * FROM documents
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY updated_at DESC
LIMIT $2;

-- name: ListDocumentsForIndexing :many
-- Every live document belonging to one person, oldest first so a rebuild
-- makes progress in a predictable order.
SELECT * FROM documents
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at;

-- name: MarkDocumentIndexed :exec
UPDATE documents
SET status         = 'ready',
    parse_error    = '',
    content_sha256 = $2,
    line_count     = $3,
    indexed_at     = now()
WHERE id = $1;

-- name: MarkDocumentFailed :exec
-- updated_at is left alone: a failed parse did not change the document, and
-- touching it would make the row look stale forever afterwards.
UPDATE documents
SET status      = 'failed',
    parse_error = $2,
    indexed_at  = now()
WHERE id = $1;

-- name: SoftDeleteDocument :exec
UPDATE documents
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: UpsertChunk :exec
-- A chunk id is derived from its content, so an unchanged chunk collides with
-- its own row and there is nothing to write. That is what makes reindexing an
-- unedited document cost no writes at all.
INSERT INTO document_chunks (
    chunk_id, document_id, user_id, ordinal, heading_path,
    start_line, end_line, content, content_sha256
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (chunk_id) DO NOTHING;

-- name: DeleteChunksNotIn :execrows
-- Removes what the latest chunking no longer produces: the passages of a
-- document that were edited away. Runs after the upserts, so a chunk is never
-- absent from the index in between.
DELETE FROM document_chunks
WHERE document_id = @document_id
  AND chunk_id <> ALL(@keep::text[]);

-- name: DeleteChunksForDocument :execrows
DELETE FROM document_chunks
WHERE document_id = $1;

-- name: SearchChunks :many
-- Passages ranked against what the user just said.
--
-- The AND-to-OR swap and the exact tsvector expression are explained in
-- internal/memories/db/queries.sql and internal/search/rank.go. They must stay
-- identical here: this table's index is built on the same expression, and the
-- 0..1 normalised rank is what lets a chunk and a profile fact be compared.
WITH q AS (
    SELECT replace(
        websearch_to_tsquery('english', @query::text)::text,
        '&', '|'
    )::tsquery AS tsq
)
SELECT
    c.chunk_id,
    c.document_id,
    c.heading_path,
    c.start_line,
    c.end_line,
    c.content,
    d.title,
    ts_headline(
        'english',
        c.content,
        q.tsq,
        'StartSel=[, StopSel=], MaxWords=35, MinWords=15, ShortWord=3, HighlightAll=FALSE'
    )::text AS snippet,
    ts_rank_cd(to_tsvector('english', c.content), q.tsq, 32)::float8 AS rank
FROM document_chunks c
JOIN documents d ON d.id = c.document_id
   , q
WHERE c.user_id = @user_id
  AND d.deleted_at IS NULL
  AND to_tsvector('english', c.content) @@ q.tsq
ORDER BY rank DESC, c.document_id, c.ordinal
LIMIT @result_limit;

-- name: GetChunk :one
SELECT
    c.chunk_id,
    c.document_id,
    c.heading_path,
    c.start_line,
    c.end_line,
    c.content,
    d.title
FROM document_chunks c
JOIN documents d ON d.id = c.document_id
WHERE c.chunk_id = $1 AND c.user_id = $2 AND d.deleted_at IS NULL;

-- name: DocumentCounts :one
-- What the knowledge page reports. Stale means the document has changed since
-- it was last indexed, so the coach is reading an out-of-date view of it.
SELECT
    count(*) FILTER (WHERE status = 'ready')::int   AS ready,
    count(*) FILTER (WHERE status = 'pending')::int AS pending,
    count(*) FILTER (WHERE status = 'failed')::int  AS failed,
    count(*) FILTER (
        WHERE status = 'ready' AND (indexed_at IS NULL OR indexed_at < updated_at)
    )::int AS stale
FROM documents
WHERE user_id = $1 AND deleted_at IS NULL;

-- name: CountChunks :one
SELECT count(*)::int FROM document_chunks WHERE user_id = $1;

-- name: StartIndexRun :one
INSERT INTO index_runs (user_id, kind)
VALUES ($1, $2)
RETURNING *;

-- name: CompleteIndexRun :exec
UPDATE index_runs
SET completed_at        = now(),
    documents_seen      = $2,
    documents_added     = $3,
    documents_updated   = $4,
    documents_unchanged = $5,
    documents_failed    = $6,
    chunks_written      = $7,
    chunks_removed      = $8,
    warnings            = $9,
    success             = $10,
    error_summary       = $11
WHERE id = $1;

-- name: LatestIndexRun :one
SELECT * FROM index_runs
WHERE user_id = $1
ORDER BY started_at DESC
LIMIT 1;

-- name: UpsertChunkEmbedding :exec
INSERT INTO chunk_embeddings (chunk_id, user_id, provider, model, embedding)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (chunk_id) DO UPDATE
SET provider   = EXCLUDED.provider,
    model      = EXCLUDED.model,
    embedding  = EXCLUDED.embedding,
    created_at = now();

-- name: ChunksNeedingEmbedding :many
-- Passages with no vector, or one from a model North is no longer using.
--
-- A vector from another model is worse than none: cosine distance between two
-- coordinate systems is a number, and it will rank things confidently and
-- wrongly. So a model change makes every row stale rather than mixed.
SELECT c.chunk_id, c.content
FROM document_chunks c
JOIN documents d ON d.id = c.document_id
LEFT JOIN chunk_embeddings e ON e.chunk_id = c.chunk_id
WHERE c.user_id = @user_id
  AND d.deleted_at IS NULL
  AND (e.chunk_id IS NULL OR e.model <> @model)
ORDER BY c.document_id, c.ordinal
LIMIT @result_limit;

-- name: SearchChunksByVector :many
-- Nearest passages by cosine distance.
--
-- Only rows embedded with the current model are considered, for the reason
-- above. Distance is returned rather than similarity so the ordering reads the
-- way pgvector writes it; the caller converts.
SELECT
    c.chunk_id,
    c.document_id,
    c.heading_path,
    c.start_line,
    c.end_line,
    c.content,
    d.title,
    (e.embedding <=> @query_vector::vector)::float8 AS distance
FROM chunk_embeddings e
JOIN document_chunks c ON c.chunk_id = e.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE e.user_id = @user_id
  AND e.model = @model
  AND d.deleted_at IS NULL
ORDER BY e.embedding <=> @query_vector::vector
LIMIT @result_limit;

-- name: CountEmbeddedChunks :one
SELECT count(*)::int FROM chunk_embeddings WHERE user_id = $1 AND model = $2;
