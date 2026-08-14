package documents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/documents/parse"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func uploadIndexFixture(t *testing.T, email string) (*documents.Service, *documents.Indexer, users.User, context.Context) {
	t.Helper()
	pool := testdb.New(t)
	store := newMemStorage()
	user := seedUser(t, pool, email)
	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, store, nil)
	indexer := documents.NewIndexer(repo, store)
	return svc, indexer, user, context.Background()
}

func handleIndexDocument(t *testing.T, indexer *documents.Indexer, ctx context.Context, user users.User, doc documents.Document) {
	t.Helper()
	payload, err := json.Marshal(jobs.IndexDocumentPayload{
		UserID:     user.ID,
		DocumentID: doc.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.HandleIndexDocument(ctx, payload); err != nil {
		t.Fatal(err)
	}
}

func handleEmbedJob(t *testing.T, embedder *documents.Embedder, ctx context.Context, user users.User) {
	t.Helper()
	payload, err := json.Marshal(jobs.EmbedChunksPayload{UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = embedder.HandleEmbedJob(ctx, payload); err != nil {
		t.Fatal(err)
	}
}

// uploadSemanticFixture is upload + hybrid retrieval: same worker handlers the
// production queue uses, plus embedding so Search exercises the full path.
func uploadSemanticFixture(t *testing.T, email string) (*documents.Service, *documents.Indexer, *documents.Embedder, users.User, context.Context) {
	t.Helper()
	pool := testdb.New(t)
	store := newMemStorage()
	user := seedUser(t, pool, email)
	repo := documents.NewRepository(pool)
	fake := fakeEmbedder{dims: 1024}
	svc := documents.NewService(repo, store, nil).WithEmbeddings(fake, nil)
	indexer := documents.NewIndexer(repo, store)
	embedder := documents.NewEmbedder(repo, fake, nil)
	return svc, indexer, embedder, user, context.Background()
}

func TestUploadedMarkdownBecomesRetrievable(t *testing.T) {
	svc, indexer, user, ctx := uploadIndexFixture(t, "upload-index-md@north.test")

	body := "# Training log\n\nNarrow grip overhead press is fine.\n"
	doc, err := svc.Upload(ctx, user.ID, "training-log.md", "text/markdown", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != documents.StatusPending {
		t.Fatalf("status = %q, want pending", doc.Status)
	}

	handleIndexDocument(t, indexer, ctx, user, doc)

	after, err := svc.Get(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != documents.StatusReady {
		t.Fatalf("status = %q, parse error = %q", after.Status, after.ParseError)
	}

	hits, err := svc.Search(ctx, user.ID, "narrow grip overhead", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("uploaded markdown was not retrieved after indexing")
	}
	if !strings.Contains(hits[0].Content, "Narrow grip") {
		t.Errorf("top hit = %q", hits[0].Content)
	}
}

func TestUploadedTextBecomesRetrievable(t *testing.T) {
	svc, indexer, user, ctx := uploadIndexFixture(t, "upload-index-txt@north.test")

	body := "Daily check-in: slept well, energy high.\n"
	doc, err := svc.Upload(ctx, user.ID, "check-in.txt", "text/plain", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	handleIndexDocument(t, indexer, ctx, user, doc)

	after, err := svc.Get(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != documents.StatusReady {
		t.Fatalf("status = %q, parse error = %q", after.Status, after.ParseError)
	}

	hits, err := svc.Search(ctx, user.ID, "energy high", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("uploaded text was not retrieved after indexing")
	}
}

func TestUploadedPDFBecomesRetrievable(t *testing.T) {
	svc, indexer, user, ctx := uploadIndexFixture(t, "upload-index-pdf@north.test")

	pdf := readPDF(t, "physio-report.pdf")
	doc, err := svc.Upload(ctx, user.ID, "physio-report.pdf", "application/pdf", bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}

	handleIndexDocument(t, indexer, ctx, user, doc)

	after, err := svc.Get(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != documents.StatusReady {
		t.Fatalf("status = %q, parse error = %q", after.Status, after.ParseError)
	}

	hits, err := svc.Search(ctx, user.ID, "shoulder assessment", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("uploaded PDF was not retrieved after indexing")
	}
	if !strings.Contains(strings.ToLower(hits[0].Content), "shoulder") {
		t.Errorf("top hit = %q", hits[0].Content)
	}
}

func TestUploadedScanPDFIsRecordedAsFailed(t *testing.T) {
	svc, indexer, user, ctx := uploadIndexFixture(t, "upload-index-scan@north.test")

	pdf := readPDF(t, "scan-no-text.pdf")
	doc, err := svc.Upload(ctx, user.ID, "scan.pdf", "application/pdf", bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}

	handleIndexDocument(t, indexer, ctx, user, doc)

	after, err := svc.Get(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != documents.StatusFailed {
		t.Fatalf("status = %q, want failed", after.Status)
	}
	if !strings.Contains(after.ParseError, parse.ErrNoText.Error()) {
		t.Errorf("parse error = %q, want scan message", after.ParseError)
	}

	problems, err := svc.Attention(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range problems {
		if p.DocumentID == doc.ID {
			found = true
			if p.Kind != documents.ProblemFailed {
				t.Errorf("kind = %q, want %q", p.Kind, documents.ProblemFailed)
			}
		}
	}
	if !found {
		t.Errorf("scanned PDF is not in the attention list: %#v", problems)
	}
}

func TestUploadedMarkdownIsSemanticallyRetrievable(t *testing.T) {
	svc, indexer, embedder, user, ctx := uploadSemanticFixture(t, "upload-semantic-md@north.test")

	body := "# Training log\n\nNarrow grip overhead press is fine.\n"
	doc, err := svc.Upload(ctx, user.ID, "training-log.md", "text/markdown", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	handleIndexDocument(t, indexer, ctx, user, doc)
	handleEmbedJob(t, embedder, ctx, user)

	hits, err := svc.Search(ctx, user.ID, "overhead press grip", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("uploaded markdown was not retrieved after index and embed")
	}
	if !strings.Contains(hits[0].Content, "Narrow grip") {
		t.Errorf("top hit = %q", hits[0].Content)
	}
}

func TestUploadedTextIsSemanticallyRetrievable(t *testing.T) {
	svc, indexer, embedder, user, ctx := uploadSemanticFixture(t, "upload-semantic-txt@north.test")

	body := "Daily check-in: slept well, energy high.\n"
	doc, err := svc.Upload(ctx, user.ID, "check-in.txt", "text/plain", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	handleIndexDocument(t, indexer, ctx, user, doc)
	handleEmbedJob(t, embedder, ctx, user)

	hits, err := svc.Search(ctx, user.ID, "high energy sleep", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("uploaded text was not retrieved after index and embed")
	}
	if !strings.Contains(hits[0].Content, "energy high") {
		t.Errorf("top hit = %q", hits[0].Content)
	}
}

func TestUploadedPDFIsSemanticallyRetrievable(t *testing.T) {
	svc, indexer, embedder, user, ctx := uploadSemanticFixture(t, "upload-semantic-pdf@north.test")

	pdf := readPDF(t, "physio-report.pdf")
	doc, err := svc.Upload(ctx, user.ID, "physio-report.pdf", "application/pdf", bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}

	handleIndexDocument(t, indexer, ctx, user, doc)
	handleEmbedJob(t, embedder, ctx, user)

	hits, err := svc.Search(ctx, user.ID, "shoulder overhead pressing", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("uploaded PDF was not retrieved after index and embed")
	}
	if !strings.Contains(strings.ToLower(hits[0].Content), "shoulder") {
		t.Errorf("top hit = %q", hits[0].Content)
	}
}
