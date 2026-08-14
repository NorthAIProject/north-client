package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// embeddingDims is the width of chunk_embeddings.embedding. A vector of any
// other length is refused here so the failure names the mismatch, rather than
// arriving as a Postgres error about a column the caller has never seen.
const embeddingDims = 1024

// embedBatchSize bounds one pass. Small enough that a provider outage costs a
// few seconds rather than a stalled worker, large enough that the round trip is
// amortised over real work.
const embedBatchSize = 64

// Embedder computes and stores vectors for a person's passages.
//
// Separate from Indexer, and running after it, because the two fail
// differently. Chunking is local, deterministic, and always succeeds or tells
// you exactly why. Embedding is a network call to somebody else's service that
// may be down, rate limited, or simply not configured — and a passage must be
// searchable by text before any of that resolves.
//
// So a missing vector is never an error state. It degrades retrieval to
// full-text, which is what North did entirely until this existed.
type Embedder struct {
	repo   *Repository
	client ai.Embedder
	log    *slog.Logger
}

func NewEmbedder(repo *Repository, client ai.Embedder, log *slog.Logger) *Embedder {
	if log == nil {
		log = slog.Default()
	}
	return &Embedder{repo: repo, client: client, log: log}
}

// Enabled reports whether embeddings are configured at all.
func (e *Embedder) Enabled() bool { return e != nil && e.client != nil }

// EmbedPending computes vectors for everything this person is missing.
//
// Passages with a vector from a different model count as missing: two models
// are two coordinate systems, and a cosine distance between them is a number
// that will rank things confidently and wrongly.
//
// Returns how many were written. A partial pass is a success — the next run
// picks up where it stopped, and the alternative is a person whose library
// never finishes because one batch keeps failing.
func (e *Embedder) EmbedPending(ctx context.Context, userID uuid.UUID) (int, error) {
	if !e.Enabled() {
		return 0, nil
	}
	if dims := e.client.Dimensions(); dims != 0 && dims != embeddingDims {
		return 0, fmt.Errorf("embedding model width %d does not match the store (%d)", dims, embeddingDims)
	}

	model := e.client.EmbedModel()
	written := 0

	for {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}

		pending, err := e.repo.ChunksNeedingEmbedding(ctx, userID, model, embedBatchSize)
		if err != nil {
			return written, err
		}
		if len(pending) == 0 {
			return written, nil
		}

		texts := make([]string, len(pending))
		for i, c := range pending {
			texts[i] = c.Content
		}

		vectors, err := e.client.Embed(ctx, texts)
		if err != nil {
			// Written vectors stay written. Returning the error stops this
			// pass; the queue's retry brings us back to exactly the rows still
			// missing, because "missing" is computed rather than tracked.
			return written, apperr.Wrap(err, "embed passages")
		}
		if len(vectors) != len(pending) {
			return written, apperr.New("embedding count did not match the passages sent")
		}
		for i, vec := range vectors {
			if len(vec) != embeddingDims {
				return written, fmt.Errorf("embedding %d has width %d, want %d", i, len(vec), embeddingDims)
			}
		}

		for i, c := range pending {
			if err := e.repo.SaveEmbedding(ctx, c.ChunkID, userID, e.client.Name(), model, vectors[i]); err != nil {
				return written, err
			}
			written++
		}

		// A short batch means there was nothing more to ask for.
		if len(pending) < embedBatchSize {
			return written, nil
		}
	}
}

// HandleEmbedJob adapts the queue payload.
func (e *Embedder) HandleEmbedJob(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode embed job")
	}

	written, err := e.EmbedPending(ctx, p.UserID)
	if written > 0 {
		e.log.Info("embedded passages",
			slog.Int("written", written),
			slog.String("model", e.client.EmbedModel()))
	}
	return err
}
