-- +goose Up

-- Semantic retrieval beside the full-text kind, not instead of it.
--
-- Full-text finds a passage that uses the user's words. It cannot find the one
-- that says "pressing overhead flares it up" when they asked about their
-- shoulder hurting on shoulder press. Embeddings find that, and miss the exact
-- term FTS gets right every time. Neither subsumes the other, so both run and
-- their rankings are fused — see internal/search/hybrid.go.
--
-- The image has shipped pgvector since the beginning (docker-compose.yml), so
-- this costs no infrastructure change.
CREATE EXTENSION IF NOT EXISTS vector;

-- Its own table rather than a column on document_chunks, for three reasons.
--
-- A vector is derived from a chunk but is not part of it: dropping every row
-- here degrades retrieval back to full-text and loses nothing that cannot be
-- recomputed, which is the same claim the chunks themselves make about the
-- documents.
--
-- Embeddings arrive later than chunks. They need a network call to a provider
-- that may be down, rate limited, or not configured at all, and a chunk must be
-- searchable before that call succeeds.
--
-- And the model is part of the data. Vectors from two models are not
-- comparable, so a model change has to invalidate what came before rather than
-- silently mixing coordinate systems — which is the failure that produces
-- confident nonsense rather than an error.
CREATE TABLE chunk_embeddings (
    chunk_id  text PRIMARY KEY REFERENCES document_chunks(chunk_id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Provider and model that produced this vector. A row whose model differs
    -- from the configured one is stale and gets recomputed.
    provider text NOT NULL,
    model    text NOT NULL,

    -- 1024 covers the retrieval models North is likely to use: NVIDIA's
    -- nv-embedqa-e5-v5 and llama-3.2-nv-embedqa are both 1024. A model of a
    -- different width needs a migration, which is the honest cost of a fixed
    -- column — and the alternative, an unsized vector column, cannot be
    -- indexed at all.
    embedding vector(1024) NOT NULL,

    created_at timestamptz NOT NULL DEFAULT now()
);

-- HNSW rather than IVFFlat: it needs no training pass over existing data, which
-- matters when the table starts empty and fills in gradually behind a queue.
--
-- vector_cosine_ops because the query side normalises nothing; cosine ignores
-- magnitude, which is what makes it the right measure for text embeddings.
CREATE INDEX idx_chunk_embeddings_vector
    ON chunk_embeddings
    USING hnsw (embedding vector_cosine_ops);

CREATE INDEX idx_chunk_embeddings_user ON chunk_embeddings (user_id);

-- +goose Down

DROP TABLE chunk_embeddings;
-- The extension is deliberately left in place: dropping it would break any
-- other schema that came to depend on it, and an unused extension costs
-- nothing.
