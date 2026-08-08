// Package documents is the knowledge a person brings to North: training logs,
// physio notes, a programme somebody handed them.
//
// A document is stored once and never rewritten. Everything else here —
// parsing, chunking, the rows in document_chunks — is derived from it, and
// exists to answer one question well: when this person asks something, which
// passages of what they have written bear on it, and where exactly did each
// come from.
//
// Three properties are load-bearing, and the tests exist to hold them.
//
// Chunks quote their own line ranges exactly. A citation that points at the
// wrong lines is worse than no citation, because it looks like evidence.
//
// Chunk ids are deterministic. Reindexing unchanged text rewrites nothing, and
// a chunk id cited in a reply months ago still resolves to the passage that
// produced it.
//
// Retrieval never writes. Indexing is the only thing in this package that
// changes a row, and it runs on the queue, never on the path of a reply.
package documents
