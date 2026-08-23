package documents

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents/parse"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/search"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/limits"
	"github.com/NorthAIProject/north-client/internal/spend"
)

const (
	listDefault = 100

	// searchLimit bounds what one turn may contribute. Small deliberately: the
	// point is the few passages that bear on the question, and a coach handed
	// twenty of them has been handed the document back.
	searchLimit = 6

	// searchPageLimit is the default page size on the dedicated search page.
	searchPageLimit = 20

	// maxUploadBytes is what Khepri will read from an upload. Text documents are
	// small; anything larger is a file somebody chose by mistake, and reading
	// it costs memory before anything has looked at what it is.
	maxUploadBytes = 8 << 20 // 8 MiB

	maxTitleLen = 200
	maxNoteLen  = 500_000
)

// acceptedExtensions is what the parser can actually read. Kept narrow rather
// than permissive: a format nobody has taught the parser would be indexed as
// its own binary, and the coach would cite the gibberish with total confidence.
var acceptedExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true,
	".txt": true, ".text": true, ".log": true, ".csv": true,
	".pdf": true,
}

// QueryEmbedder embeds a search query.
//
// Narrower than ai.Embedder on purpose: retrieval needs one method, and a
// query is embedded differently from a passage — most retrieval models are
// asymmetric, and using the passage side for a question costs recall silently.
type QueryEmbedder interface {
	EmbedModel() string
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// Service owns documents and the retrieval over them.
type Service struct {
	repo    *Repository
	storage Storage
	queue   *jobs.Queue

	// opts must match the Indexer's, and both take the zero value. Held here
	// only so Attention can name a document whose chunks came from bounds the
	// running code no longer uses; nothing in this type chunks anything.
	opts Options

	// embeddings is nil unless a provider is configured. Retrieval then runs
	// full-text only, which is what Khepri did before embeddings existed.
	embeddings QueryEmbedder
	logger     *slog.Logger
}

func NewService(repo *Repository, storage Storage, queue *jobs.Queue) *Service {
	return &Service{repo: repo, storage: storage, queue: queue}
}

// WithEmbeddings turns on the semantic half of retrieval.
//
// A separate call rather than a constructor argument so that every existing
// caller keeps working and gets the previous behaviour, which is the correct
// behaviour when no embedding provider is configured.
func (s *Service) WithEmbeddings(embedder QueryEmbedder, log *slog.Logger) *Service {
	s.embeddings = embedder
	s.logger = log
	return s
}

// CreateNote stores a document written inside Khepri.
func (s *Service) CreateNote(ctx context.Context, userID uuid.UUID, title, body string) (Document, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)

	var errs apperr.FieldErrors
	if title == "" {
		errs = errs.Add("title", "Give this note a name you'll recognise later.")
	} else if len(title) > maxTitleLen {
		errs = errs.Add("title", "Keep the name under 200 characters.")
	}
	switch {
	case body == "":
		errs = errs.Add("body", "Write something for Khepri to remember.")
	case len(body) > maxNoteLen:
		errs = errs.Add("body", "This note is too long. Split it into a few.")
	}
	if err := errs.OrNil(); err != nil {
		return Document{}, err
	}

	doc, err := s.repo.Create(ctx, userID, NewDocument{
		Title:      title,
		SourceKind: SourceNote,
		Body:       body,
		MIME:       "text/markdown",
		ByteSize:   int64(len(body)),
	})
	if err != nil {
		return Document{}, err
	}

	s.enqueueIndex(ctx, doc)
	return doc, nil
}

// Upload stores an uploaded file and queues it for indexing.
//
// The bytes go to object storage and are never copied into the database: the
// file the person handed over stays exactly as they handed it over, and
// everything Khepri derives from it can be thrown away and rebuilt.
func (s *Service) Upload(ctx context.Context, userID uuid.UUID, filename, mime string, body io.Reader) (Document, error) {
	filename = filepath.Base(strings.TrimSpace(filename))

	ext := strings.ToLower(filepath.Ext(filename))
	if !acceptedExtensions[ext] {
		return Document{}, apperr.FieldErrors{}.Add("file",
			"Khepri can read Markdown, plain text, and PDF files.").OrNil()
	}
	if s.storage == nil {
		return Document{}, fmt.Errorf("documents: no object storage configured")
	}

	// LimitReader plus one byte, so an oversized file is reported rather than
	// silently truncated into a document missing its ending.
	content, err := io.ReadAll(io.LimitReader(body, maxUploadBytes+1))
	if err != nil {
		return Document{}, apperr.Wrap(err, "read upload")
	}
	if len(content) > maxUploadBytes {
		return Document{}, apperr.FieldErrors{}.Add("file",
			"That file is larger than 8 MB.").OrNil()
	}
	if len(content) == 0 {
		return Document{}, apperr.FieldErrors{}.Add("file", "That file is empty.").OrNil()
	}

	// The declared MIME is ignored: browsers send application/octet-stream
	// for .md files, and an extension can be attached to anything. Sniff the
	// bytes the way media does, and store that.
	detected, err := classifyUpload(ext, content)
	if err != nil {
		return Document{}, err
	}

	key := fmt.Sprintf("users/%s/documents/%s%s", userID, uuid.New(), ext)
	if putErr := s.storage.Put(ctx, key, detected, bytes.NewReader(content)); putErr != nil {
		return Document{}, apperr.Wrap(putErr, "store document")
	}

	doc, err := s.repo.Create(ctx, userID, NewDocument{
		Title:      parseTitle(filename, detected, string(content)),
		SourceKind: SourceUpload,
		StorageKey: key,
		MIME:       detected,
		ByteSize:   int64(len(content)),
	})
	if err != nil {
		// The object is now orphaned. Removing it is best-effort: a stray blob
		// costs pennies, and failing the upload a second time over cleanup
		// would tell the user something untrue about their own file.
		_ = s.storage.Delete(ctx, key)
		return Document{}, err
	}

	s.enqueueIndex(ctx, doc)
	return doc, nil
}

func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (Document, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Document, error) {
	return s.repo.List(ctx, userID, listDefault)
}

// Content returns a document and the parsed text a citation is numbered
// against.
//
// The parsed document, not the raw bytes. parse.Document.Lines is the exact
// sequence the chunker counted, so line 40 here is line 40 in the citation. For
// a PDF the two are nothing alike — the raw bytes are a binary container — and
// for a markdown file with front matter they differ by the width of the block.
// Showing anything else would produce a page that highlights the wrong lines
// while looking entirely correct.
func (s *Service) Content(ctx context.Context, id, userID uuid.UUID) (Document, parse.Document, error) {
	doc, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return Document{}, parse.Document{}, err
	}

	raw, err := readContent(ctx, s.storage, doc)
	if err != nil {
		return doc, parse.Document{}, err
	}

	parsed, err := parse.Parse(titleSource(doc), doc.MIME, raw)
	if err != nil {
		return doc, parse.Document{}, err
	}
	return doc, parsed, nil
}

// Chunk resolves one citation to the passage it names.
func (s *Service) Chunk(ctx context.Context, chunkID string, userID uuid.UUID) (Hit, error) {
	return s.repo.Chunk(ctx, chunkID, userID)
}

// Passages resolves the citations of one reply together.
//
// Ids that no longer resolve are absent rather than an error: a document
// deleted since the reply was written should cost that reply its sources, not
// make the conversation unreadable.
func (s *Service) Passages(ctx context.Context, userID uuid.UUID, chunkIDs []string) ([]Hit, error) {
	return s.repo.ChunksByIDs(ctx, userID, chunkIDs)
}

// Delete removes a document from the person's knowledge.
//
// The row is soft-deleted and its chunks go with it, because a chunk left
// behind would still be retrievable and the coach would keep citing something
// its owner believes they deleted. The stored bytes are removed too: this is
// the delete a person asked for, not an archive.
func (s *Service) Delete(ctx context.Context, id, userID uuid.UUID) error {
	doc, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, id, userID); err != nil {
		return err
	}
	if _, _, err := s.repo.ReplaceChunks(ctx, doc, nil); err != nil {
		return err
	}
	if doc.StorageKey != "" && s.storage != nil {
		if err := s.storage.Delete(ctx, doc.StorageKey); err != nil {
			return apperr.Wrap(err, "remove stored document")
		}
	}
	return nil
}

// Search returns the passages that bear on a query.
//
// Read-only, and structurally so: nothing on this path writes a row. Retrieval
// runs on every reply, and a retrieval that could mutate is a retrieval that
// can corrupt the thing it is reading from under a person mid-conversation.
//
// Full-text always runs. When embeddings are configured, a vector search runs
// beside it and the two rankings are fused. Neither method subsumes the other:
// full-text finds the passage that uses the person's words, vectors find the
// one that means the same thing in different words, and each misses what the
// other catches.
//
// If the embedding provider is unreachable the vector half is skipped and the
// full-text result stands. A slow or broken provider must never cost somebody
// an answer they would otherwise have got.
func (s *Service) Search(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = searchLimit
	}
	hits, _, err := s.SearchPage(ctx, userID, query, limit, 0)
	return hits, err
}

// SearchPage returns one page of hybrid results and whether more exist beyond it.
func (s *Service) SearchPage(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]Hit, bool, error) {
	normalised, err := search.Normalise(query)
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = searchPageLimit
	}
	if limit, err = limits.Validate(limit, "limit", limits.MaxSearchLimit); err != nil {
		return nil, false, err
	}
	if offset < 0 {
		offset = 0
	}

	need := min(offset+limit+1, limits.MaxSearchLimit)
	deep := min(need*3, limits.MaxSearchLimit)

	fused, err := s.fusedSearch(ctx, userID, normalised, deep)
	if err != nil {
		return nil, false, err
	}

	hasMore := len(fused) > offset+limit
	if offset >= len(fused) {
		return nil, hasMore, nil
	}
	end := min(offset+limit, len(fused))
	return fused[offset:end], hasMore, nil
}

func (s *Service) fusedSearch(ctx context.Context, userID uuid.UUID, normalised string, deep int) ([]Hit, error) {
	textHits, err := s.repo.Search(ctx, userID, normalised, deep)
	if err != nil {
		return nil, err
	}

	vectorHits := s.vectorSearch(ctx, userID, normalised, deep)
	if len(vectorHits) == 0 {
		return textHits, nil
	}

	return search.Fuse(textHits, vectorHits), nil
}

// vectorSearch is best-effort by design; see Search.
func (s *Service) vectorSearch(ctx context.Context, userID uuid.UUID, query string, limit int) []Hit {
	if s.embeddings == nil {
		return nil
	}

	// A search embeds the query too. Small next to indexing, but it happens on
	// every search, so leaving it unattributed would put a per-request cost in
	// the unattributed bucket forever.
	vector, err := s.embeddings.EmbedQuery(aiattr.WithUser(ctx, userID, spend.SurfaceEmbedding), query)
	if err != nil {
		s.log().Warn("semantic search skipped: could not embed the query", slog.Any("error", err))
		return nil
	}
	if len(vector) != embeddingDims {
		s.log().Warn("semantic search skipped: query vector width does not match the store",
			slog.Int("got", len(vector)), slog.Int("want", embeddingDims))
		return nil
	}

	hits, err := s.repo.SearchByVector(ctx, userID, s.embeddings.EmbedModel(), vector, limit)
	if err != nil {
		s.log().Warn("semantic search skipped", slog.Any("error", err))
		return nil
	}
	return hits
}

func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// Counts and LatestRun back the knowledge page and the MCP status tool.
func (s *Service) Counts(ctx context.Context, userID uuid.UUID) (Counts, error) {
	return s.repo.Counts(ctx, userID)
}

func (s *Service) LatestRun(ctx context.Context, userID uuid.UUID) (IndexRun, error) {
	return s.repo.LatestRun(ctx, userID)
}

// stuckAfter is how long a document may sit unread before it is worth naming.
//
// Indexing runs on the queue and normally finishes in seconds. Long enough that
// a busy worker is not reported as broken, short enough that a worker which is
// not running is visible while the person is still on the page.
const stuckAfter = 10 * time.Minute

// Attention lists the documents that need something done to them.
//
// Computed from the rows the page already loads rather than from a query: every
// input is a column on the document, and a second trip to the database to
// re-derive what is in memory would be work for its own sake.
//
// One problem per document, worst first: a document that failed to parse is not
// also usefully described as stale, and a list that says the same file three
// ways is a list nobody reads to the end.
func (s *Service) Attention(ctx context.Context, userID uuid.UUID) ([]Problem, error) {
	docs, err := s.repo.List(ctx, userID, listDefault)
	if err != nil {
		return nil, err
	}

	fingerprint := s.opts.Fingerprint()
	out := make([]Problem, 0)

	for _, d := range docs {
		p := Problem{DocumentID: d.ID, Title: d.Title}

		switch {
		case d.Status == StatusFailed:
			p.Kind = ProblemFailed
			p.Detail = d.ParseError
			if p.Detail == "" {
				p.Detail = "Khepri could not read this one."
			}

		case d.Status == StatusPending && time.Since(d.CreatedAt) > stuckAfter:
			p.Kind = ProblemStuck
			p.Detail = "Queued to be read, and still waiting."

		case d.Status == StatusReady && d.LineCount == 0:
			p.Kind = ProblemEmpty
			p.Detail = "Khepri read this and found no text in it."

		case d.IsStale():
			p.Kind = ProblemStale
			p.Detail = "This changed after Khepri last read it."

		case d.ReadByAnotherChunker(fingerprint):
			p.Kind = ProblemOldReader
			p.Detail = "Khepri reads documents differently now. Re-reading this will improve what it finds."

		default:
			continue
		}

		out = append(out, p)
	}
	return out, nil
}

// EmbeddingGap is how many passages are searchable by word but not by meaning.
//
// Reported as one number rather than per document because a missing vector is
// almost never about one file: it is a provider that was down, or a backfill
// that has not caught up. Zero when no embedding provider is configured, where
// the gap is not a gap but the design.
func (s *Service) EmbeddingGap(ctx context.Context, userID uuid.UUID) int {
	if s.embeddings == nil {
		return 0
	}

	counts, err := s.repo.Counts(ctx, userID)
	if err != nil {
		return 0
	}

	embedded, err := s.repo.CountEmbedded(ctx, userID, s.embeddings.EmbedModel())
	if err != nil {
		return 0
	}

	return max(counts.Chunks-embedded, 0)
}

// Reindex rebuilds everything derived for one person.
//
// The reason this exists is the claim the schema makes: chunks are derived
// state. A claim like that is only true if there is a button that proves it.
func (s *Service) Reindex(ctx context.Context, userID uuid.UUID) error {
	if s.queue == nil {
		return fmt.Errorf("documents: no queue configured")
	}
	_, err := s.queue.Enqueue(ctx, jobs.KindReindexUser, jobs.ReindexUserPayload{UserID: userID})
	return err
}

// ReindexDocument rebuilds one document's chunks.
//
// The per-document form of Reindex, for the case the attention list names: a
// single file Khepri read badly, or read under bounds it no longer uses. Fetches
// the row first so that a request naming somebody else's document, or one that
// has been deleted, enqueues nothing.
func (s *Service) ReindexDocument(ctx context.Context, id, userID uuid.UUID) error {
	doc, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return err
	}
	if s.queue == nil {
		return fmt.Errorf("documents: no queue configured")
	}

	_, err = s.queue.Enqueue(ctx, jobs.KindIndexDocument, jobs.IndexDocumentPayload{
		UserID:     doc.UserID,
		DocumentID: doc.ID,
	})
	return err
}

// enqueueIndex schedules indexing off the request path.
//
// A failure to enqueue leaves the document pending rather than failing the
// upload: the file is stored and the person owns it, and a reindex will pick it
// up. Losing the upload over a queue hiccup would be the worse outcome.
func (s *Service) enqueueIndex(ctx context.Context, doc Document) {
	if s.queue == nil {
		return
	}
	_, _ = s.queue.Enqueue(ctx, jobs.KindIndexDocument, jobs.IndexDocumentPayload{
		UserID:     doc.UserID,
		DocumentID: doc.ID,
	})
}

// parseTitle reads the document far enough to find a name for it.
//
// A parse failure here is not fatal: the upload is accepted, named after its
// file, and the indexer reports the real problem where the person can see it.
// Refusing the upload would lose their file over a diagnosis they have not been
// shown yet.
func parseTitle(filename, mime, content string) string {
	// A PDF is not parsed here. Extracting one costs the same work the indexer
	// is about to do on the queue, and doing it twice — once on the request
	// path — to guess a name is not worth the wait.
	if strings.EqualFold(filepath.Ext(filename), ".pdf") {
		return titleFromFilename(filename)
	}

	doc, err := parse.Parse(filename, mime, content)
	if err != nil || strings.TrimSpace(doc.Title) == "" {
		return titleFromFilename(filename)
	}
	return doc.Title
}

// classifyUpload decides what an upload actually is.
//
// Extension is the allowlist; the leading bytes are the type. A .pdf that is
// not a PDF, or a .txt full of NULs, is refused here rather than stored and
// later indexed as gibberish.
func classifyUpload(ext string, content []byte) (string, error) {
	header := content
	if len(header) > 512 {
		header = header[:512]
	}
	sniffed := normaliseMIME(http.DetectContentType(header))

	if ext == ".pdf" {
		if sniffed != "application/pdf" && !bytes.HasPrefix(content, []byte("%PDF")) {
			return "", apperr.FieldErrors{}.Add("file",
				"That does not look like a PDF Khepri can read.").OrNil()
		}
		return "application/pdf", nil
	}

	binary := bytes.IndexByte(content, 0) >= 0 || sniffed == "application/octet-stream"
	pdf := sniffed == "application/pdf" || bytes.HasPrefix(content, []byte("%PDF"))
	if binary || pdf {
		return "", apperr.FieldErrors{}.Add("file",
			"That file is not readable text.").OrNil()
	}
	return mimeForExt(ext), nil
}

func mimeForExt(ext string) string {
	switch ext {
	case ".md", ".markdown", ".mdown":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	default:
		return "text/plain"
	}
}

// normaliseMIME strips the charset parameter DetectContentType can append.
func normaliseMIME(mimeType string) string {
	if base, _, found := strings.Cut(mimeType, ";"); found {
		return strings.TrimSpace(base)
	}
	return mimeType
}

// titleFromFilename mirrors the parser's fallback for the case above.
func titleFromFilename(filename string) string {
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.NewReplacer("-", " ", "_", " ").Replace(base)
	if base = strings.TrimSpace(base); base == "" {
		return "Untitled"
	}
	return base
}
