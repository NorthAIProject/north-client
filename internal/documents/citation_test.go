package documents_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// The feature, asserted end to end.
//
// A citation says "lines 40-58 of your physio notes". This reads those lines
// back out of the document the source view renders and requires them to be the
// passage the coach was given, byte for byte. If this drifts, the page shows a
// highlight over the wrong text while looking entirely correct, which is worse
// than showing nothing: it is a wrong answer wearing evidence.
func TestACitationResolvesToTheLinesItNames(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "citation-roundtrip@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(ctx, user.ID, "band pull-aparts", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("nothing retrieved for a term that is in the document")
	}

	_, parsed, err := svc.Content(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, hit := range hits {
		if hit.StartLine < 1 || hit.EndLine > len(parsed.Lines) {
			t.Fatalf("hit cites lines %d-%d of a %d-line document",
				hit.StartLine, hit.EndLine, len(parsed.Lines))
		}

		quoted := strings.Join(parsed.Lines[hit.StartLine-1:hit.EndLine], "\n")
		if quoted != hit.Content {
			t.Errorf("lines %d-%d of the document are not the passage that was cited\n got: %q\nwant: %q",
				hit.StartLine, hit.EndLine, quoted, hit.Content)
		}
	}
}

// The same, for the ref stored on a reply rather than a fresh search result.
func TestAStoredRefResolvesToItsPassage(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "citation-stored@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(ctx, user.ID, "overhead pressing", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("nothing retrieved")
	}
	want := hits[0]

	got, err := svc.Chunk(ctx, want.ChunkID, user.ID)
	if err != nil {
		t.Fatalf("a chunk id that was just handed out does not resolve: %v", err)
	}
	if got.Content != want.Content || got.StartLine != want.StartLine || got.EndLine != want.EndLine {
		t.Errorf("resolved a different passage than was cited:\n got %d-%d %q\nwant %d-%d %q",
			got.StartLine, got.EndLine, got.Content, want.StartLine, want.EndLine, want.Content)
	}

	// The batch form is what a reply's sources actually go through.
	batch, err := svc.Passages(ctx, user.ID, []string{want.ChunkID})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 || batch[0].ChunkID != want.ChunkID {
		t.Fatalf("Passages returned %d hit(s) for one id", len(batch))
	}
}

// A ref that no longer resolves must cost a reply its source, not break the
// conversation it is part of.
func TestPassagesSkipRefsThatNoLongerResolve(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "citation-gone@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(ctx, user.ID, "overhead pressing", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("nothing retrieved")
	}
	live := hits[0].ChunkID

	got, err := svc.Passages(ctx, user.ID, []string{live, "nor_chk_deadbeef"})
	if err != nil {
		t.Fatalf("one dead ref failed the whole lookup: %v", err)
	}
	if len(got) != 1 || got[0].ChunkID != live {
		t.Errorf("got %d hit(s), want only the one that still exists", len(got))
	}

	// A deleted document takes its passages with it.
	if err = svc.Delete(ctx, doc.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Passages(ctx, user.ID, []string{live})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("a deleted document still resolves %d passage(s)", len(after))
	}
}

// Chunk ids appear in stored replies and in logs. The row filter is the only
// thing standing between one person's ref and another person's document.
func TestChunkLookupStaysInsideOneAccount(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, nil, nil)
	indexer := documents.NewIndexer(repo, nil)

	owner := seedUser(t, pool, "citation-owner@north.test")
	intruder := seedUser(t, pool, "citation-intruder@north.test")

	doc, err := svc.CreateNote(ctx, owner.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, owner.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(ctx, owner.ID, "overhead pressing", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("nothing retrieved")
	}
	stolen := hits[0].ChunkID

	if _, err = svc.Chunk(ctx, stolen, intruder.ID); err == nil {
		t.Error("another account resolved a chunk id it does not own")
	}

	batch, err := svc.Passages(ctx, intruder.ID, []string{stolen})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 0 {
		t.Errorf("another account read %d passage(s) it does not own", len(batch))
	}
}

// The attention list is the part of the knowledge page that says which document
// needs something, rather than how many do.
func TestAttentionNamesAFailedDocument(t *testing.T) {
	svc, indexer, repo, user, ctx := fixture(t, "attention-failed@north.test")

	// An upload whose bytes cannot be read back. Written through the
	// repository because the service refuses one at the boundary — which is
	// the right place to refuse it, and leaves this the way a document
	// actually ends up failed: stored once, unreadable later.
	doc, err := repo.Create(ctx, user.ID, documents.NewDocument{
		Title:      "A file that went missing",
		SourceKind: documents.SourceUpload,
		StorageKey: "users/" + user.ID.String() + "/documents/gone.md",
		MIME:       "text/markdown",
		ByteSize:   120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	after, err := svc.Get(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != documents.StatusFailed {
		t.Fatalf("status = %q, want %q", after.Status, documents.StatusFailed)
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
			if p.Detail == "" {
				t.Error("a problem with no detail tells its owner nothing")
			}
		}
	}
	if !found {
		t.Errorf("the failed document is not in the attention list: %#v", problems)
	}
}

// A document read by bounds the running code no longer uses is not broken, and
// the list has to say so without calling it an error.
func TestAttentionNamesADocumentReadAnOlderWay(t *testing.T) {
	svc, indexer, repo, user, ctx := fixture(t, "attention-old-reader@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	// Nothing is in the list while the reader matches.
	problems, err := svc.Attention(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		if p.DocumentID == doc.ID {
			t.Fatalf("a freshly indexed document is already in the attention list as %q", p.Kind)
		}
	}

	// Rewrite the fingerprint to stand in for a chunker whose bounds changed.
	after, err := svc.Get(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkIndexed(ctx, doc.ID, after.ContentSHA256, "some-older-reader", after.LineCount); err != nil {
		t.Fatal(err)
	}

	problems, err = svc.Attention(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, p := range problems {
		if p.DocumentID == doc.ID {
			found = true
			if p.Kind != documents.ProblemOldReader {
				t.Errorf("kind = %q, want %q", p.Kind, documents.ProblemOldReader)
			}
		}
	}
	if !found {
		t.Errorf("a document read by an older chunker is not in the attention list: %#v", problems)
	}
}

// The reindex that clears it must not be reachable across accounts.
func TestReindexDocumentStaysInsideOneAccount(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := documents.NewService(documents.NewRepository(pool), nil, nil)

	owner := seedUser(t, pool, "reindex-owner@north.test")
	intruder := seedUser(t, pool, "reindex-intruder@north.test")

	doc, err := svc.CreateNote(ctx, owner.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ReindexDocument(ctx, doc.ID, intruder.ID); err == nil {
		t.Error("another account queued a reindex of a document it does not own")
	}
}
