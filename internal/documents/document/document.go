// Package document holds the shape of a piece of knowledge a person has given
// North.
//
// A leaf package with no dependencies of its own, so the templates under web/
// can render a document without importing the service that manages one — the
// same reason internal/memories/memory exists.
package document

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Where a document's text lives.
const (
	// SourceUpload keeps its bytes in object storage.
	SourceUpload = "upload"

	// SourceNote was written in North and lives in the database.
	SourceNote = "note"
)

// Indexing states.
const (
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

// Document is one piece of knowledge a person has given North.
type Document struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Title      string
	SourceKind string
	StorageKey string
	Body       string
	MIME       string
	ByteSize   int64

	// ContentSHA256 fingerprints the text that was last chunked. It is what
	// lets a reindex recognise a document it has already done and skip it.
	ContentSHA256 string

	LineCount  int
	Status     string
	ParseError string

	IndexedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsStale reports whether the document has changed since it was last indexed.
//
// A stale document is not broken — the coach can still read the previous
// version of it — but it is reading something the person no longer has, which
// is worth saying out loud on the knowledge page.
func (d Document) IsStale() bool {
	return d.Status == StatusReady && (d.IndexedAt == nil || d.IndexedAt.Before(d.UpdatedAt))
}

// ContentUnchanged reports whether the document's text is the one already
// indexed, and so whether there is any work to do.
func (d Document) ContentUnchanged(sha string) bool {
	return d.ContentSHA256 != "" && d.ContentSHA256 == sha
}

// Hit is one retrieved passage, with everything needed to cite it.
type Hit struct {
	ChunkID     string
	DocumentID  uuid.UUID
	Title       string
	HeadingPath []string

	StartLine int
	EndLine   int

	// Content is the passage as written; Snippet is the excerpt with matched
	// terms bracketed. The model is given Content, the reader the Snippet.
	Content string
	Snippet string

	// Rank is 0..1 and comparable with a memory's rank.
	Rank float64
}

// Key identifies a passage across retrieval methods, so full-text and vector
// results for the same passage fuse into one entry rather than two.
func (h Hit) Key() string { return h.ChunkID }

// Label names the source of a hit the way a person would.
//
// "Training log › Deload weeks" rather than a chunk id: a citation nobody can
// read is a citation nobody will check.
func (h Hit) Label() string {
	var b strings.Builder
	b.WriteString("note: ")
	b.WriteString(h.Title)
	for _, part := range h.HeadingPath {
		// The document's title is usually also its top heading; repeating it
		// reads as a stutter.
		if strings.EqualFold(part, h.Title) {
			continue
		}
		b.WriteString(" › ")
		b.WriteString(part)
	}
	return b.String()
}

// IndexRun records what one pass of the indexer did.
type IndexRun struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Kind   string

	StartedAt   time.Time
	CompletedAt *time.Time

	Seen, Added, Updated, Unchanged, Failed int
	ChunksWritten, ChunksRemoved            int

	Warnings     []string
	Success      bool
	ErrorSummary string
}

// Counts is the summary shown on the knowledge page.
type Counts struct {
	Ready, Pending, Failed, Stale int
	Chunks                        int
}
