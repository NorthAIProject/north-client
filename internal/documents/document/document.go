// Package document holds the shape of a piece of knowledge a person has given
// North.
//
// A leaf package with no dependencies of its own, so the templates under web/
// can render a document without importing the service that manages one — the
// same reason internal/memories/memory exists.
package document

import (
	"fmt"
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

	// ChunkerFingerprint identifies the bounds those chunks were produced
	// under. Empty means the document was indexed before North recorded this,
	// which is indistinguishable from having been read by a reader that no
	// longer exists — and is treated as such.
	ChunkerFingerprint string

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

// IndexedWith reports whether the chunks on disk were produced from this text
// by this reader, and so whether there is any work to do.
//
// Both halves matter. Same text, different bounds, is the case that used to
// pass silently: the document would keep chunks nothing in the running code
// would produce, and go on reporting that it had been read.
func (d Document) IndexedWith(sha, fingerprint string) bool {
	return d.ContentSHA256 != "" &&
		d.ContentSHA256 == sha &&
		d.ChunkerFingerprint == fingerprint
}

// ReadByAnotherChunker reports whether this document's chunks came from bounds
// the running code no longer uses.
//
// Not an error and not stale in the sense IsStale means — the text is the text
// the person wrote. It is the index that is behind, and a reindex fixes it.
func (d Document) ReadByAnotherChunker(fingerprint string) bool {
	return d.Status == StatusReady && d.ChunkerFingerprint != fingerprint
}

// Hit is one retrieved passage, with everything needed to cite it.
type Hit struct {
	ChunkID     string
	DocumentID  uuid.UUID
	Title       string
	HeadingPath []string

	StartLine int
	EndLine   int

	// Content is the passage as written; Snippet is the excerpt with the
	// matched terms wrapped in MarkStart and MarkEnd. The model is given
	// Content, the reader the Snippet.
	Content string
	Snippet string

	// Rank is 0..1 and comparable with a memory's rank.
	Rank float64
}

// Key identifies a passage across retrieval methods, so full-text and vector
// results for the same passage fuse into one entry rather than two.
func (h Hit) Key() string { return h.ChunkID }

// URL points at the passage in the document it came from.
//
// The query pair drives the highlight and the fragment drives the scroll; the
// source view needs both, because a browser will not scroll to a line it has
// been given no anchor for. Line numbers are the document's own, which is what
// makes this checkable rather than decorative.
func (h Hit) URL() string {
	return fmt.Sprintf("/app/knowledge/%s?from=%d&to=%d#L%d",
		h.DocumentID, h.StartLine, h.EndLine, h.StartLine)
}

// The markers ts_headline wraps a matched term in. See the snippet expression
// in internal/documents/db/queries.sql for why they are control characters.
const (
	MarkStart = "\x02"
	MarkEnd   = "\x03"
)

// Segment is one run of a snippet, either matched or not.
//
// Returned as data rather than as HTML so the template escapes it like any
// other text. Building the markup here would mean handing templ a string it
// must be told to trust, over text that came out of a person's own file.
type Segment struct {
	Text    string
	Matched bool
}

// Segments splits a snippet into its matched and unmatched runs.
//
// Unbalanced markers are treated as ordinary text, which is the safe direction:
// a snippet that ends mid-match renders without emphasis rather than swallowing
// the rest of the excerpt.
func (h Hit) Segments() []Segment {
	var (
		out  []Segment
		rest = h.Snippet
	)

	for {
		start := strings.Index(rest, MarkStart)
		if start < 0 {
			break
		}
		end := strings.Index(rest[start:], MarkEnd)
		if end < 0 {
			break
		}
		end += start

		if start > 0 {
			out = append(out, Segment{Text: rest[:start]})
		}
		out = append(out, Segment{Text: rest[start+len(MarkStart) : end], Matched: true})
		rest = rest[end+len(MarkEnd):]
	}

	if rest != "" {
		out = append(out, Segment{Text: rest})
	}
	return out
}

// Lines names a passage's range the way a person cites one.
func (h Hit) Lines() string {
	if h.StartLine == h.EndLine {
		return fmt.Sprintf("L%d", h.StartLine)
	}
	return fmt.Sprintf("L%d–%d", h.StartLine, h.EndLine)
}

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

// Problem kinds, worst first — the order they are reported in.
const (
	// ProblemFailed: North could not read the file at all.
	ProblemFailed = "failed"

	// ProblemEmpty: read successfully, and there was nothing in it.
	ProblemEmpty = "empty"

	// ProblemStale: the text changed after North last read it, so the coach is
	// quoting a version its owner no longer has.
	ProblemStale = "stale"

	// ProblemOldReader: the chunks came from bounds the running code no longer
	// uses. Nothing is wrong with the document; the index is behind the code.
	ProblemOldReader = "old-reader"

	// ProblemStuck: queued to be read and still waiting well past when it
	// should have been. Usually a worker that is not running.
	ProblemStuck = "stuck"
)

// Problem is one document that needs something done to it.
//
// The knowledge page could already say four documents are stale. It could not
// say which, or why, or offer to fix one — which makes the number a worry
// rather than a task.
type Problem struct {
	DocumentID uuid.UUID
	Title      string
	Kind       string

	// Detail is the reason in words its owner can act on, and for a failed
	// document it is the parser's own message.
	Detail string
}
