-- +goose Up

-- Which reader produced this document's chunks.
--
-- content_sha256 answers "has the text changed since it was indexed". It cannot
-- answer "has the way North reads text changed", and that is the failure worth
-- naming: raise DefaultMaxChars and every existing document keeps the chunks
-- the old bounds produced, reports 'ready', and is never looked at again.
--
-- Empty for every row written before this column existed, which is correct — a
-- document indexed by an unknown reader is exactly the thing to offer a reindex
-- for. Nothing is rechunked automatically: the old chunk ids stay alive, so the
-- citations already quoted in stored replies keep resolving until their owner
-- asks for the reindex.
ALTER TABLE documents ADD COLUMN chunker_fingerprint text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE documents DROP COLUMN chunker_fingerprint;
