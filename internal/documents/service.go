package documents

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents/parse"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/search"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/limits"
)

const (
	listDefault = 100

	// searchLimit bounds what one turn may contribute. Small deliberately: the
	// point is the few passages that bear on the question, and a coach handed
	// twenty of them has been handed the document back.
	searchLimit = 6

	// maxUploadBytes is what North will read from an upload. Text documents are
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

// Service owns documents and the retrieval over them.
// QueryEmbedder embeds a search query.
//
// Narrower than ai.Embedder on purpose: retrieval needs one method, and a
// query is embedded differently from a passage — most retrieval models are
// asymmetric, and using the passage side for a question costs recall silently.
type QueryEmbedder interface {
	EmbedModel() string
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

type Service struct {
	repo    *Repository
	storage Storage
	queue   *jobs.Queue

	// embeddings is nil unless a provider is configured. Retrieval then runs
	// full-text only, which is what North did before embeddings existed.
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

// CreateNote stores a document written inside North.
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
		errs = errs.Add("body", "Write something for North to remember.")
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
// everything North derives from it can be thrown away and rebuilt.
func (s *Service) Upload(ctx context.Context, userID uuid.UUID, filename, mime string, body io.Reader) (Document, error) {
	filename = filepath.Base(strings.TrimSpace(filename))

	ext := strings.ToLower(filepath.Ext(filename))
	if !acceptedExtensions[ext] {
		return Document{}, apperr.FieldErrors{}.Add("file",
			"North can read Markdown, plain text, and PDF files.").OrNil()
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

	key := fmt.Sprintf("users/%s/documents/%s%s", userID, uuid.New(), ext)
	if err := s.storage.Put(ctx, key, mime, strings.NewReader(string(content))); err != nil {
		return Document{}, apperr.Wrap(err, "store document")
	}

	doc, err := s.repo.Create(ctx, userID, NewDocument{
		Title:      parseTitle(filename, mime, string(content)),
		SourceKind: SourceUpload,
		StorageKey: key,
		MIME:       mime,
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
	normalised, err := search.Normalise(query)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = searchLimit
	}
	if limit, err = limits.Validate(limit, "limit", limits.MaxSearchLimit); err != nil {
		return nil, err
	}

	// Each retriever is asked for more than the final count: fusion only has
	// something to work with if the lists overlap, and two lists of six rarely
	// do.
	deep := min(limit*3, limits.MaxSearchLimit)

	textHits, err := s.repo.Search(ctx, userID, normalised, deep)
	if err != nil {
		return nil, err
	}

	vectorHits := s.vectorSearch(ctx, userID, normalised, deep)
	if len(vectorHits) == 0 {
		return truncate(textHits, limit), nil
	}

	return truncate(search.Fuse(textHits, vectorHits), limit), nil
}

// vectorSearch is best-effort by design; see Search.
func (s *Service) vectorSearch(ctx context.Context, userID uuid.UUID, query string, limit int) []Hit {
	if s.embeddings == nil {
		return nil
	}

	vector, err := s.embeddings.EmbedQuery(ctx, query)
	if err != nil {
		s.log().Warn("semantic search skipped: could not embed the query", slog.Any("error", err))
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

func truncate(hits []Hit, limit int) []Hit {
	if len(hits) > limit {
		return hits[:limit]
	}
	return hits
}

// Counts and LatestRun back the knowledge page and the MCP status tool.
func (s *Service) Counts(ctx context.Context, userID uuid.UUID) (Counts, error) {
	return s.repo.Counts(ctx, userID)
}

func (s *Service) LatestRun(ctx context.Context, userID uuid.UUID) (IndexRun, error) {
	return s.repo.LatestRun(ctx, userID)
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
