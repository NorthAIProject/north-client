package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents/parse"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Indexer turns stored documents into retrievable chunks.
//
// It is the only thing in this package that writes chunk rows, and it runs on
// the queue rather than on a request. Indexing a document is bounded work, but
// reindexing a person's whole library is not, and neither belongs in the time a
// user spends looking at a spinner.
type Indexer struct {
	repo    *Repository
	storage Storage
	opts    Options
}

func NewIndexer(repo *Repository, storage Storage) *Indexer {
	return &Indexer{repo: repo, storage: storage}
}

// IndexDocument indexes one document and records the run.
func (ix *Indexer) IndexDocument(ctx context.Context, userID, documentID uuid.UUID) error {
	doc, err := ix.repo.Get(ctx, documentID, userID)
	if err != nil {
		return err
	}

	run, err := ix.repo.StartRun(ctx, userID, "document")
	if err != nil {
		return err
	}
	run.Seen = 1

	ix.indexOne(ctx, doc, &run)

	run.Success = run.Failed == 0
	return ix.repo.CompleteRun(ctx, run)
}

// ReindexUser rebuilds every chunk this person has.
//
// One document failing does not stop the pass. A run that abandoned the rest of
// a library because one file was unreadable would leave the person with a
// partly-indexed account and no way to tell which part.
func (ix *Indexer) ReindexUser(ctx context.Context, userID uuid.UUID) error {
	docs, err := ix.repo.ForIndexing(ctx, userID)
	if err != nil {
		return err
	}

	run, err := ix.repo.StartRun(ctx, userID, "user")
	if err != nil {
		return err
	}
	run.Seen = len(docs)

	for _, doc := range docs {
		if ctx.Err() != nil {
			run.ErrorSummary = "the run was cancelled part-way"
			break
		}
		ix.indexOne(ctx, doc, &run)
	}

	run.Success = run.Failed == 0 && run.ErrorSummary == ""
	return ix.repo.CompleteRun(ctx, run)
}

// indexOne parses, chunks, and stores one document, folding what happened into
// the run rather than returning it: a per-document failure is a fact to record,
// not a reason to abandon the pass.
func (ix *Indexer) indexOne(ctx context.Context, doc Document, run *IndexRun) {
	content, err := ix.read(ctx, doc)
	if err != nil {
		ix.fail(ctx, doc, run, err.Error())
		return
	}
	if strings.TrimSpace(content) == "" {
		ix.fail(ctx, doc, run, "this document has no readable text in it")
		return
	}

	parsed := parseDoc(titleSource(doc), doc.MIME, content)
	sum := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(sum[:])

	// An unchanged document that is already indexed has nothing to do. This is
	// what makes a full reindex cheap, and what the idempotence test asserts.
	if doc.Status == StatusReady && doc.ContentUnchanged(sha) {
		run.Unchanged++
		return
	}

	chunks := ChunkDocument(parsed, ix.opts)
	if len(chunks) == 0 {
		ix.fail(ctx, doc, run, "this document produced nothing to search")
		return
	}

	written, removed, err := ix.repo.ReplaceChunks(ctx, doc, chunks)
	run.ChunksWritten += written
	run.ChunksRemoved += removed
	if err != nil {
		ix.fail(ctx, doc, run, "North could not store this document's contents")
		return
	}

	if err := ix.repo.MarkIndexed(ctx, doc.ID, sha, parsed.LineCount()); err != nil {
		ix.fail(ctx, doc, run, "North could not record that this document was indexed")
		return
	}

	if doc.IndexedAt == nil {
		run.Added++
	} else {
		run.Updated++
	}
}

func (ix *Indexer) fail(ctx context.Context, doc Document, run *IndexRun, reason string) {
	run.Failed++
	run.Warnings = append(run.Warnings, fmt.Sprintf("%s: %s", doc.Title, reason))
	_ = ix.repo.MarkFailed(ctx, doc.ID, reason)
}

// read returns a document's text, from the database for a note and from object
// storage for an upload.
func (ix *Indexer) read(ctx context.Context, doc Document) (string, error) {
	if doc.SourceKind == SourceNote {
		return doc.Body, nil
	}
	if ix.storage == nil {
		return "", fmt.Errorf("North has no file storage configured")
	}

	body, err := ix.storage.Get(ctx, doc.StorageKey)
	if err != nil {
		return "", fmt.Errorf("North could not read this file back from storage")
	}
	defer func() { _ = body.Close() }()

	content, err := io.ReadAll(io.LimitReader(body, maxUploadBytes))
	if err != nil {
		return "", fmt.Errorf("North could not read this file back from storage")
	}
	return string(content), nil
}

// titleSource is the filename the parser should use when the document has no
// title of its own inside it.
func titleSource(doc Document) string {
	if doc.StorageKey != "" {
		return doc.StorageKey
	}
	return doc.Title
}

func parseDoc(filename, mime, content string) parse.Document {
	return parse.Parse(filename, mime, content)
}

// HandleIndexDocument and HandleReindexUser adapt the queue's payloads.
func (ix *Indexer) HandleIndexDocument(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		UserID     uuid.UUID `json:"user_id"`
		DocumentID uuid.UUID `json:"document_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode index job")
	}
	return ix.IndexDocument(ctx, p.UserID, p.DocumentID)
}

func (ix *Indexer) HandleReindexUser(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode reindex job")
	}
	return ix.ReindexUser(ctx, p.UserID)
}
