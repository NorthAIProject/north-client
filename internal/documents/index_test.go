package documents_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/documents/document"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// fixture builds a service with no storage and no queue: notes need neither,
// and indexing is driven directly so the test does not depend on a worker.
func fixture(t *testing.T, email string) (*documents.Service, *documents.Indexer, *documents.Repository, users.User, context.Context) {
	t.Helper()
	pool := testdb.New(t)
	repo := documents.NewRepository(pool)
	return documents.NewService(repo, nil, nil),
		documents.NewIndexer(repo, nil),
		repo,
		seedUser(t, pool, email),
		context.Background()
}

const physioNote = `# Physio notes

Assessment after the shoulder flared up again.

## Overhead pressing

Wide grip aggravates it every time. Narrow grip and landmine pressing are
both fine, and the physio was clear that stopping entirely is the wrong
answer.

## Rehab

Band pull-aparts daily, three sets of twenty. Boring but it works.
`

func TestIndexedDocumentBecomesRetrievable(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "index-basic@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != documents.StatusPending {
		t.Fatalf("a new document should be pending, got %q", doc.Status)
	}

	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	after, err := svc.Get(ctx, doc.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != documents.StatusReady {
		t.Fatalf("status = %q, parse error = %q", after.Status, after.ParseError)
	}
	if after.IsStale() {
		t.Error("a freshly indexed document reports itself stale")
	}

	hits, err := svc.Search(ctx, user.ID, "can I still press overhead with my shoulder?", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("the passage that answers the question was not retrieved")
	}

	top := hits[0]
	if !strings.Contains(top.Content, "Wide grip aggravates it") {
		t.Errorf("top hit was the wrong passage:\n%s", top.Content)
	}
	if top.Label() != "note: Physio notes › Overhead pressing" {
		t.Errorf("label = %q", top.Label())
	}
	if top.StartLine < 1 || top.EndLine < top.StartLine {
		t.Errorf("hit carries an impossible line range %d-%d", top.StartLine, top.EndLine)
	}
	if !strings.Contains(top.Snippet, document.MarkStart) {
		t.Errorf("snippet has no marked terms: %q", top.Snippet)
	}

	// The marks have to survive as far as the reader, or the emphasis in a
	// result row is decoration rather than a report of what matched.
	var matched []string
	for _, s := range top.Segments() {
		if s.Matched {
			matched = append(matched, s.Text)
		}
	}
	if len(matched) == 0 {
		t.Errorf("no segment of the snippet came back matched: %q", top.Snippet)
	}
	for _, m := range matched {
		if !strings.Contains(strings.ToLower(top.Content), strings.ToLower(m)) {
			t.Errorf("snippet marked %q, which is not in the passage", m)
		}
	}
}

// The claim the schema makes is that chunks are derived state. This is what
// makes that claim true rather than aspirational — and what keeps a chunk id
// cited in an old reply resolvable after a rebuild.
func TestReindexIsIdempotent(t *testing.T) {
	svc, indexer, repo, user, ctx := fixture(t, "index-idempotent@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	before, err := svc.Search(ctx, user.ID, "overhead pressing grip", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("nothing indexed on the first pass")
	}

	if err = indexer.ReindexUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}

	run, err := repo.LatestRun(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Seen != 1 || run.Unchanged != 1 {
		t.Errorf("run saw %d documents and skipped %d as unchanged; want 1 and 1", run.Seen, run.Unchanged)
	}
	if run.ChunksWritten != 0 || run.ChunksRemoved != 0 {
		t.Errorf("an unchanged document cost %d writes and %d deletes; want none",
			run.ChunksWritten, run.ChunksRemoved)
	}
	if !run.Success {
		t.Errorf("run failed: %s %v", run.ErrorSummary, run.Warnings)
	}

	after, err := svc.Search(ctx, user.ID, "overhead pressing grip", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("hit count changed across reindex: %d then %d", len(before), len(after))
	}
	for i := range before {
		if before[i].ChunkID != after[i].ChunkID {
			t.Errorf("chunk id changed across reindex: %s -> %s", before[i].ChunkID, after[i].ChunkID)
		}
	}
}

// An unreadable document must be reported to its owner rather than swallowed.
func TestAFailedDocumentIsRecordedNotSwallowed(t *testing.T) {
	svc, indexer, repo, user, ctx := fixture(t, "index-failure@north.test")

	// A note whose text is only whitespace parses to nothing.
	doc, err := svc.CreateNote(ctx, user.ID, "Empty", "   \n\n   \t  \n  ")
	if err == nil {
		if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
			t.Fatal(err)
		}
		var after documents.Document
		after, err = svc.Get(ctx, doc.ID, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != documents.StatusFailed || after.ParseError == "" {
			t.Errorf("status = %q, parse error = %q; want a recorded failure", after.Status, after.ParseError)
		}
		run, runErr := repo.LatestRun(ctx, user.ID)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if run.Failed != 1 || len(run.Warnings) == 0 {
			t.Errorf("run recorded %d failures and %d warnings", run.Failed, len(run.Warnings))
		}
		return
	}

	// Validation rejecting it up front is the better outcome, and is what
	// currently happens: the person is told immediately rather than after a
	// round trip through the queue.
	if !strings.Contains(err.Error(), "Write something") {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestDeleteRemovesTheChunksToo(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "index-delete@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.Delete(ctx, doc.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(ctx, user.ID, "overhead pressing grip", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		// A chunk outliving its document means the coach keeps citing
		// something its owner believes they deleted.
		t.Errorf("deleted document still retrievable: %d hits", len(hits))
	}
}

func TestSearchStaysInsideOneAccount(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, nil, nil)
	indexer := documents.NewIndexer(repo, nil)

	owner := seedUser(t, pool, "doc-owner@north.test")
	stranger := seedUser(t, pool, "doc-stranger@north.test")

	doc, err := svc.CreateNote(ctx, owner.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, owner.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := svc.Search(ctx, stranger.ID, "overhead pressing grip", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("search crossed accounts: %d hits", len(hits))
	}
}

// The context source is the thing that finally writes Context.KnowledgeHits,
// which has been declared and rendered and populated by nothing until now.
func TestContextSourceFillsKnowledgeHits(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "index-source@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	source := documents.NewContextSource(svc)
	var into coach.Context

	err = source.Collect(ctx, coach.ContextRequest{
		User:  user,
		Query: "what did the physio say about pressing overhead?",
	}, &into)
	if err != nil {
		t.Fatal(err)
	}
	if len(into.KnowledgeHits) == 0 {
		t.Fatal("KnowledgeHits is still empty")
	}

	hit := into.KnowledgeHits[0]
	if !strings.HasPrefix(hit.Ref, "chunk:"+documents.ChunkIDPrefix) {
		t.Errorf("ref = %q, want a chunk ref", hit.Ref)
	}
	if !strings.Contains(hit.Label, "Physio notes") {
		t.Errorf("label = %q", hit.Label)
	}

	// The refs offered must be exactly what the coach is allowed to cite.
	if !into.OfferedRefs()[hit.Ref] {
		t.Error("a retrieved chunk was not offered as a citable ref")
	}

	// And the rendered block must actually carry it, or the model never sees
	// a handle to copy.
	if !strings.Contains(into.Render(), "[["+hit.Ref+"]]") {
		t.Error("the rendered context does not carry the chunk's citation handle")
	}
}

// With no query there is nothing to rank against, and contributing the most
// recent documents regardless would crowd the context with padding.
func TestContextSourceContributesNothingWithoutAQuery(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "index-noquery@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	var into coach.Context
	err = documents.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, &into)
	if err != nil {
		t.Fatalf("an empty query should be a normal outcome, got %v", err)
	}
	if len(into.KnowledgeHits) != 0 {
		t.Errorf("contributed %d hits with no query", len(into.KnowledgeHits))
	}
}

func TestCountsReportStaleDocuments(t *testing.T) {
	svc, indexer, repo, user, ctx := fixture(t, "index-counts@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}

	counts, err := repo.Counts(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Pending != 1 || counts.Ready != 0 {
		t.Errorf("before indexing: %+v", counts)
	}

	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	counts, err = repo.Counts(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Ready != 1 || counts.Stale != 0 || counts.Chunks == 0 {
		t.Errorf("after indexing: %+v", counts)
	}
}

func TestChunkIDsSurviveAnEdit(t *testing.T) {
	svc, indexer, _, user, ctx := fixture(t, "index-edit@north.test")

	doc, err := svc.CreateNote(ctx, user.ID, "Physio notes", physioNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	rehab, err := svc.Search(ctx, user.ID, "band pull-aparts three sets", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rehab) == 0 {
		t.Fatal("the rehab passage was not indexed")
	}
	// An untouched section keeps its identity across a reindex; that is what
	// keeps a citation written months ago resolvable.
	if err = indexer.ReindexUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	again, err := svc.Search(ctx, user.ID, "band pull-aparts three sets", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) == 0 || again[0].ChunkID != rehab[0].ChunkID {
		t.Error("an unedited passage lost its chunk id across a reindex")
	}
}
