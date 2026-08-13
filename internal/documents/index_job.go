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
	"github.com/NorthAIProject/north-client/internal/jobs"
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

	// queue is nil unless embeddings are wired. When present, a pass that
	// wrote chunks queues the vector work behind it rather than doing it
	// inline: a provider outage must not fail an index that otherwise
	// succeeded, and the passages are already searchable by text.
	queue *jobs.Queue
}

func NewIndexer(repo *Repository, storage Storage) *Indexer {
	return &Indexer{repo: repo, storage: storage}
}

// WithEmbeddingQueue makes the indexer schedule vector work after each pass.
func (ix *Indexer) WithEmbeddingQueue(queue *jobs.Queue) *Indexer {
	ix.queue = queue
	return ix
}

// queueEmbeddings schedules the vector pass for whatever this run changed.
//
// Failures are logged by the queue and otherwise ignored: the chunks are
// written and searchable, and a missing vector costs recall rather than
// correctness.
func (ix *Indexer) queueEmbeddings(ctx context.Context, userID uuid.UUID, run IndexRun) {
	if ix.queue == nil || run.ChunksWritten == 0 {
		return
	}
	_, _ = ix.queue.Enqueue(ctx, jobs.KindEmbedChunks, jobs.EmbedChunksPayload{UserID: userID})
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
	ix.queueEmbeddings(ctx, userID, run)
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
	ix.queueEmbeddings(ctx, userID, run)
	return ix.repo.CompleteRun(ctx, run)
}

// indexOne parses, chunks, and stores one document, folding what happened into
// the run rather than returning it: a per-document failure is a fact to record,
// not a reason to abandon the pass.
func (ix *Indexer) indexOne(ctx context.Context, doc Document, run *IndexRun) {
	content, err := readContent(ctx, ix.storage, doc)
	if err != nil {
		ix.fail(ctx, doc, run, err.Error())
		return
	}
	if strings.TrimSpace(content) == "" {
		ix.fail(ctx, doc, run, "this document has no readable text in it")
		return
	}

	parsed, err := parse.Parse(titleSource(doc), doc.MIME, content)
	if err != nil {
		// A parse failure is a fact about the person's file, not an outage.
		// Its message is written for them to read on the knowledge page.
		ix.fail(ctx, doc, run, err.Error())
		return
	}
	sum := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(sum[:])

	// An unchanged document already indexed by this reader has nothing to do.
	// This is what makes a full reindex cheap, and what the idempotence test
	// asserts. The fingerprint is half the question: same text read under
	// different bounds is different chunks, and skipping it would leave the
	// document holding passages the running code would never produce.
	fingerprint := ix.opts.Fingerprint()
	if doc.Status == StatusReady && doc.IndexedWith(sha, fingerprint) {
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

	if err := ix.repo.MarkIndexed(ctx, doc.ID, sha, fingerprint, parsed.LineCount()); err != nil {
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

// readContent returns a document's text, from the database for a note and from
// object storage for an upload.
//
// A package function rather than a method because two callers need it and they
// are not the same object: the indexer, which chunks the text, and the service,
// which shows it to the person a citation points at. Two copies of this branch
// would be two ways for the view and the index to disagree about what the
// document says.
func readContent(ctx context.Context, storage Storage, doc Document) (string, error) {
	if doc.SourceKind == SourceNote {
		return doc.Body, nil
	}
	if storage == nil {
		return "", fmt.Errorf("North has no file storage configured")
	}

	body, err := storage.Get(ctx, doc.StorageKey)
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
