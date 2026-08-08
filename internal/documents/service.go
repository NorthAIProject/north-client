package documents

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

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

// acceptedMIME is what the parser can actually read. Kept narrow rather than
// permissive: a PDF accepted here would be indexed as its own binary and the
// coach would cite gibberish with total confidence.
var acceptedExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true,
	".txt": true, ".text": true, ".log": true, ".csv": true,
}

// Service owns documents and the retrieval over them.
type Service struct {
	repo    *Repository
	storage Storage
	queue   *jobs.Queue
}

func NewService(repo *Repository, storage Storage, queue *jobs.Queue) *Service {
	return &Service{repo: repo, storage: storage, queue: queue}
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
			"North can read Markdown and plain text files for now.").OrNil()
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
	return s.repo.Search(ctx, userID, normalised, limit)
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
func parseTitle(filename, mime, content string) string {
	return parseDoc(filename, mime, content).Title
}
