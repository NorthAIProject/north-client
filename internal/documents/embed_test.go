package documents_test

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// fakeEmbedder produces deterministic vectors from word overlap.
//
// Not a stand-in for a real model, and not trying to be: it exists so the
// storage, retrieval and fusion around embeddings can be tested without a
// network call or an API key. Texts sharing words land near each other, which
// is the only property the code under test depends on.
type fakeEmbedder struct{ dims int }

func (f fakeEmbedder) Name() string       { return "fake" }
func (f fakeEmbedder) EmbedModel() string { return "fake-embed-v1" }
func (f fakeEmbedder) Dimensions() int    { return f.dims }

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = f.vector(text)
	}
	return out, nil
}

func (f fakeEmbedder) EmbedQuery(_ context.Context, query string) ([]float32, error) {
	return f.vector(query), nil
}

// vector is a normalised bag of words hashed into the dimension space.
func (f fakeEmbedder) vector(text string) []float32 {
	v := make([]float32, f.dims)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.Trim(word, ".,!?;:")))
		v[int(h.Sum32())%f.dims] += 1
	}

	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		v[0] = 1
		return v
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

const embedNote = `# Training notes

## Overhead pressing

Wide grip aggravates the left shoulder every time.

## Conditioning

Rowing intervals on Tuesday, twenty minutes.
`

func TestEmbeddingsAreStoredAndSearchable(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "embed@north.test")

	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, nil, nil)
	indexer := documents.NewIndexer(repo, nil)

	doc, err := svc.CreateNote(ctx, user.ID, "Training notes", embedNote)
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	fake := fakeEmbedder{dims: 1024}
	embedder := documents.NewEmbedder(repo, fake, nil)

	written, err := embedder.EmbedPending(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if written == 0 {
		t.Fatal("no passages were embedded")
	}

	// Running again must be free: "needs embedding" is computed from what is
	// there, so a second pass has nothing to do.
	again, err := embedder.EmbedPending(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("a second pass re-embedded %d passages; it should have found none", again)
	}

	stored, err := repo.CountEmbedded(ctx, user.ID, fake.EmbedModel())
	if err != nil {
		t.Fatal(err)
	}
	if stored != written {
		t.Errorf("stored %d vectors, wrote %d", stored, written)
	}

	vector, err := fake.EmbedQuery(ctx, "wide grip shoulder")
	if err != nil {
		t.Fatal(err)
	}
	hits, err := repo.SearchByVector(ctx, user.ID, fake.EmbedModel(), vector, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("vector search returned nothing")
	}
	if !strings.Contains(hits[0].Content, "Wide grip") {
		t.Errorf("nearest passage was the wrong one:\n%s", hits[0].Content)
	}
	if hits[0].Rank < 0 || hits[0].Rank > 1 {
		t.Errorf("similarity %f is outside 0..1", hits[0].Rank)
	}
}

// A vector from another model is worse than no vector: cosine distance across
// two coordinate systems is a number, and it ranks confidently and wrongly.
func TestAModelChangeInvalidatesOldVectors(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "embed-model-change@north.test")

	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, nil, nil)
	indexer := documents.NewIndexer(repo, nil)

	doc, err := svc.CreateNote(ctx, user.ID, "Training notes", embedNote)
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	first := fakeEmbedder{dims: 1024}
	written, err := documents.NewEmbedder(repo, first, nil).EmbedPending(ctx, user.ID)
	if err != nil || written == 0 {
		t.Fatalf("first pass: %v (%d written)", err, written)
	}

	// Same vectors, different model name — which is exactly how a model swap
	// looks to the database.
	renamed, err := documents.NewEmbedder(repo, renamedEmbedder{first}, nil).EmbedPending(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if renamed != written {
		t.Errorf("a model change re-embedded %d of %d passages; it should redo all of them", renamed, written)
	}

	// And the old model's rows are gone rather than lingering beside the new.
	old, err := repo.CountEmbedded(ctx, user.ID, first.EmbedModel())
	if err != nil {
		t.Fatal(err)
	}
	if old != 0 {
		t.Errorf("%d vectors from the previous model survived the change", old)
	}
}

type renamedEmbedder struct{ fakeEmbedder }

func (renamedEmbedder) EmbedModel() string { return "fake-embed-v2" }

// With no embedder configured, everything still works and nothing is written.
func TestEmbeddingIsOptional(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "embed-off@north.test")

	repo := documents.NewRepository(pool)
	embedder := documents.NewEmbedder(repo, nil, nil)

	if embedder.Enabled() {
		t.Error("an embedder with no client reports itself enabled")
	}
	written, err := embedder.EmbedPending(ctx, user.ID)
	if err != nil {
		t.Errorf("EmbedPending with no client failed: %v", err)
	}
	if written != 0 {
		t.Errorf("wrote %d vectors with no client", written)
	}
}

// Retrieval must fuse the two methods, and must survive either one being empty.
func TestHybridSearchReturnsBothKindsOfMatch(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "embed-hybrid@north.test")

	repo := documents.NewRepository(pool)
	fake := fakeEmbedder{dims: 1024}
	svc := documents.NewService(repo, nil, nil).WithEmbeddings(fake, nil)
	indexer := documents.NewIndexer(repo, nil)

	doc, err := svc.CreateNote(ctx, user.ID, "Training notes", embedNote)
	if err != nil {
		t.Fatal(err)
	}
	if err := indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := documents.NewEmbedder(repo, fake, nil).EmbedPending(ctx, user.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(ctx, user.ID, "wide grip pressing", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid search returned nothing")
	}

	// No passage may appear twice: fusion deduplicates by chunk id, and a
	// duplicate would spend the context window saying the same thing.
	seen := map[string]bool{}
	for _, h := range hits {
		if seen[h.ChunkID] {
			t.Errorf("chunk %s appears twice in the fused results", h.ChunkID)
		}
		seen[h.ChunkID] = true
	}

	if !strings.Contains(hits[0].Content, "Wide grip") {
		t.Errorf("top hit was the wrong passage:\n%s", hits[0].Content)
	}
}
