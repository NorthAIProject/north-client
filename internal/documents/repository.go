package documents

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	documentsdb "github.com/NorthAIProject/north-client/internal/documents/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *documentsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: documentsdb.New(pool)}
}

// NewDocument is a row to insert.
type NewDocument struct {
	Title      string
	SourceKind string
	StorageKey string
	Body       string
	MIME       string
	ByteSize   int64
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, d NewDocument) (Document, error) {
	row, err := r.q.CreateDocument(ctx, documentsdb.CreateDocumentParams{
		UserID:     userID,
		Title:      d.Title,
		SourceKind: d.SourceKind,
		StorageKey: nilIfEmpty(d.StorageKey),
		Body:       nilIfEmpty(d.Body),
		Mime:       d.MIME,
		ByteSize:   d.ByteSize,
	})
	if err != nil {
		return Document{}, apperr.Wrap(err, "create document")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Document, error) {
	row, err := r.q.GetDocument(ctx, documentsdb.GetDocumentParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, apperr.ErrNotFound
		}
		return Document{}, apperr.Wrap(err, "get document")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Document, error) {
	rows, err := r.q.ListDocuments(ctx, documentsdb.ListDocumentsParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list documents")
	}
	return fromDBList(rows), nil
}

func (r *Repository) ForIndexing(ctx context.Context, userID uuid.UUID) ([]Document, error) {
	rows, err := r.q.ListDocumentsForIndexing(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list documents for indexing")
	}
	return fromDBList(rows), nil
}

func (r *Repository) MarkIndexed(ctx context.Context, id uuid.UUID, sha string, lineCount int) error {
	return apperr.Wrap(r.q.MarkDocumentIndexed(ctx, documentsdb.MarkDocumentIndexedParams{
		ID:            id,
		ContentSha256: sha,
		LineCount:     int32(lineCount),
	}), "mark document indexed")
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, reason string) error {
	return apperr.Wrap(r.q.MarkDocumentFailed(ctx, documentsdb.MarkDocumentFailedParams{
		ID:         id,
		ParseError: reason,
	}), "mark document failed")
}

func (r *Repository) SoftDelete(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.SoftDeleteDocument(ctx, documentsdb.SoftDeleteDocumentParams{
		ID:     id,
		UserID: userID,
	}), "delete document")
}

// ReplaceChunks writes the chunks a document currently produces and removes the
// ones it no longer does.
//
// Upserts first, deletes second, on purpose: doing it the other way round would
// leave the document unsearchable for the width of the write, and a reply that
// landed in that window would silently have no knowledge to draw on.
//
// Returns how many rows were written and removed, for the run record.
func (r *Repository) ReplaceChunks(ctx context.Context, doc Document, chunks []Chunk) (written, removed int, err error) {
	keep := make([]string, 0, len(chunks))

	for _, c := range chunks {
		id := ChunkID(doc.ID, c.Ordinal, c.SHA256)
		keep = append(keep, id)

		path, err := json.Marshal(c.HeadingPath)
		if err != nil {
			return written, 0, apperr.Wrap(err, "encode heading path")
		}

		if err := r.q.UpsertChunk(ctx, documentsdb.UpsertChunkParams{
			ChunkID:       id,
			DocumentID:    doc.ID,
			UserID:        doc.UserID,
			Ordinal:       int32(c.Ordinal),
			HeadingPath:   path,
			StartLine:     int32(c.StartLine),
			EndLine:       int32(c.EndLine),
			Content:       c.Content,
			ContentSha256: c.SHA256,
		}); err != nil {
			return written, 0, apperr.Wrap(err, "write chunk")
		}
		written++
	}

	n, err := r.q.DeleteChunksNotIn(ctx, documentsdb.DeleteChunksNotInParams{
		DocumentID: doc.ID,
		Keep:       keep,
	})
	if err != nil {
		return written, 0, apperr.Wrap(err, "remove stale chunks")
	}
	return written, int(n), nil
}

func (r *Repository) Search(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Hit, error) {
	rows, err := r.q.SearchChunks(ctx, documentsdb.SearchChunksParams{
		UserID:      userID,
		Query:       query,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "search document chunks")
	}

	out := make([]Hit, 0, len(rows))
	for _, row := range rows {
		out = append(out, Hit{
			ChunkID:     row.ChunkID,
			DocumentID:  row.DocumentID,
			Title:       row.Title,
			HeadingPath: decodePath(row.HeadingPath),
			StartLine:   int(row.StartLine),
			EndLine:     int(row.EndLine),
			Content:     row.Content,
			Snippet:     row.Snippet,
			Rank:        row.Rank,
		})
	}
	return out, nil
}

func (r *Repository) Counts(ctx context.Context, userID uuid.UUID) (Counts, error) {
	docs, err := r.q.DocumentCounts(ctx, userID)
	if err != nil {
		return Counts{}, apperr.Wrap(err, "count documents")
	}
	chunks, err := r.q.CountChunks(ctx, userID)
	if err != nil {
		return Counts{}, apperr.Wrap(err, "count chunks")
	}
	return Counts{
		Ready:   int(docs.Ready),
		Pending: int(docs.Pending),
		Failed:  int(docs.Failed),
		Stale:   int(docs.Stale),
		Chunks:  int(chunks),
	}, nil
}

func (r *Repository) StartRun(ctx context.Context, userID uuid.UUID, kind string) (IndexRun, error) {
	row, err := r.q.StartIndexRun(ctx, documentsdb.StartIndexRunParams{UserID: userID, Kind: kind})
	if err != nil {
		return IndexRun{}, apperr.Wrap(err, "start index run")
	}
	return runFromDB(row), nil
}

func (r *Repository) CompleteRun(ctx context.Context, run IndexRun) error {
	warnings, err := json.Marshal(orEmpty(run.Warnings))
	if err != nil {
		return apperr.Wrap(err, "encode index warnings")
	}
	return apperr.Wrap(r.q.CompleteIndexRun(ctx, documentsdb.CompleteIndexRunParams{
		ID:                 run.ID,
		DocumentsSeen:      int32(run.Seen),
		DocumentsAdded:     int32(run.Added),
		DocumentsUpdated:   int32(run.Updated),
		DocumentsUnchanged: int32(run.Unchanged),
		DocumentsFailed:    int32(run.Failed),
		ChunksWritten:      int32(run.ChunksWritten),
		ChunksRemoved:      int32(run.ChunksRemoved),
		Warnings:           warnings,
		Success:            run.Success,
		ErrorSummary:       run.ErrorSummary,
	}), "complete index run")
}

func (r *Repository) LatestRun(ctx context.Context, userID uuid.UUID) (IndexRun, error) {
	row, err := r.q.LatestIndexRun(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IndexRun{}, apperr.ErrNotFound
		}
		return IndexRun{}, apperr.Wrap(err, "latest index run")
	}
	return runFromDB(row), nil
}

func fromDBList(rows []documentsdb.Document) []Document {
	out := make([]Document, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
}

func fromDB(row documentsdb.Document) Document {
	d := Document{
		ID:         row.ID,
		UserID:     row.UserID,
		Title:      row.Title,
		SourceKind: row.SourceKind,
		MIME:       row.Mime,
		ByteSize:   row.ByteSize,

		ContentSHA256: row.ContentSha256,
		LineCount:     int(row.LineCount),
		Status:        row.Status,
		ParseError:    row.ParseError,
		IndexedAt:     row.IndexedAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	if row.StorageKey != nil {
		d.StorageKey = *row.StorageKey
	}
	if row.Body != nil {
		d.Body = *row.Body
	}
	return d
}

func runFromDB(row documentsdb.IndexRun) IndexRun {
	run := IndexRun{
		ID:            row.ID,
		UserID:        row.UserID,
		Kind:          row.Kind,
		StartedAt:     row.StartedAt,
		CompletedAt:   row.CompletedAt,
		Seen:          int(row.DocumentsSeen),
		Added:         int(row.DocumentsAdded),
		Updated:       int(row.DocumentsUpdated),
		Unchanged:     int(row.DocumentsUnchanged),
		Failed:        int(row.DocumentsFailed),
		ChunksWritten: int(row.ChunksWritten),
		ChunksRemoved: int(row.ChunksRemoved),
		Success:       row.Success,
		ErrorSummary:  row.ErrorSummary,
	}
	// A malformed warnings column should not make the run unreadable; the
	// counts are the part that matters.
	_ = json.Unmarshal(row.Warnings, &run.Warnings)
	return run
}

func decodePath(raw []byte) []string {
	var out []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// PendingChunk is a passage still waiting for a vector.
type PendingChunk struct {
	ChunkID string
	Content string
}

func (r *Repository) ChunksNeedingEmbedding(ctx context.Context, userID uuid.UUID, model string, limit int) ([]PendingChunk, error) {
	rows, err := r.q.ChunksNeedingEmbedding(ctx, documentsdb.ChunksNeedingEmbeddingParams{
		UserID:      userID,
		Model:       model,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list chunks needing embedding")
	}

	out := make([]PendingChunk, 0, len(rows))
	for _, row := range rows {
		out = append(out, PendingChunk{ChunkID: row.ChunkID, Content: row.Content})
	}
	return out, nil
}

func (r *Repository) SaveEmbedding(ctx context.Context, chunkID string, userID uuid.UUID, provider, model string, vector []float32) error {
	v := pgvector.NewVector(vector)
	return apperr.Wrap(r.q.UpsertChunkEmbedding(ctx, documentsdb.UpsertChunkEmbeddingParams{
		ChunkID:   chunkID,
		UserID:    userID,
		Provider:  provider,
		Model:     model,
		Embedding: &v,
	}), "save embedding")
}

// SearchByVector returns the passages nearest the query vector.
//
// Cosine distance comes back as 0 (identical) to 2 (opposite); it is turned
// into a 0..1 similarity here so a caller never has to remember which direction
// is better.
func (r *Repository) SearchByVector(ctx context.Context, userID uuid.UUID, model string, vector []float32, limit int) ([]Hit, error) {
	v := pgvector.NewVector(vector)

	rows, err := r.q.SearchChunksByVector(ctx, documentsdb.SearchChunksByVectorParams{
		UserID:      userID,
		Model:       model,
		QueryVector: &v,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "search chunks by vector")
	}

	out := make([]Hit, 0, len(rows))
	for _, row := range rows {
		out = append(out, Hit{
			ChunkID:     row.ChunkID,
			DocumentID:  row.DocumentID,
			Title:       row.Title,
			HeadingPath: decodePath(row.HeadingPath),
			StartLine:   int(row.StartLine),
			EndLine:     int(row.EndLine),
			Content:     row.Content,
			Rank:        1 - (row.Distance / 2),
		})
	}
	return out, nil
}

func (r *Repository) CountEmbedded(ctx context.Context, userID uuid.UUID, model string) (int, error) {
	n, err := r.q.CountEmbeddedChunks(ctx, documentsdb.CountEmbeddedChunksParams{UserID: userID, Model: model})
	if err != nil {
		return 0, apperr.Wrap(err, "count embedded chunks")
	}
	return int(n), nil
}
