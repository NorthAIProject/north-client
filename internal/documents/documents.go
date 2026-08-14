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

import (
	"context"
	"io"

	"github.com/NorthAIProject/north-client/internal/documents/document"
)

// The domain types live in the leaf package so web/knowledge can render a
// document without importing this one, which imports web/knowledge for its
// handler. Aliased here so nothing else has to know about the split.
type (
	Document = document.Document
	Hit      = document.Hit
	IndexRun = document.IndexRun
	Counts   = document.Counts
	Problem  = document.Problem
)

const (
	SourceUpload = document.SourceUpload
	SourceNote   = document.SourceNote
	SourceVault  = document.SourceVault

	StatusPending = document.StatusPending
	StatusReady   = document.StatusReady
	StatusFailed  = document.StatusFailed

	ProblemFailed    = document.ProblemFailed
	ProblemEmpty     = document.ProblemEmpty
	ProblemStale     = document.ProblemStale
	ProblemOldReader = document.ProblemOldReader
	ProblemStuck     = document.ProblemStuck
)

// Storage is the object store, declared here rather than imported from media so
// that this package depends on the two methods it uses and not on another
// slice's service.
type Storage interface {
	Put(ctx context.Context, key, contentType string, body io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
