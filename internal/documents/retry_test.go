package documents_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestRetryEmbeddingsEnqueuesJob(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "retry-embed@north.test")

	queue := jobs.NewQueue(pool)
	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, nil, queue)

	if err := svc.RetryEmbeddings(ctx, user.ID); err != nil {
		t.Fatal(err)
	}

	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim embed job: ok=%v err=%v", ok, err)
	}
	if claimed.Kind != jobs.KindEmbedChunks {
		t.Fatalf("kind = %q", claimed.Kind)
	}
}

func TestRequeueFailedEmbedJobAllowsRetry(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "requeue-embed@north.test")

	queue := jobs.NewQueue(pool)
	if _, err := queue.Enqueue(ctx, jobs.KindEmbedChunks, jobs.EmbedChunksPayload{UserID: user.ID}); err != nil {
		t.Fatal(err)
	}

	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim embed job: ok=%v err=%v", ok, err)
	}
	if err := queue.Fail(ctx, claimed.ID, "provider down"); err != nil {
		t.Fatal(err)
	}

	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, nil, queue)
	if err := svc.RetryEmbeddings(ctx, user.ID); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := queue.Claim(ctx); err != nil || !ok {
		t.Fatalf("expected requeued job, ok=%v err=%v", ok, err)
	}
}

func TestSweepEnqueuesUsersWithGap(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "sweep-embed@north.test")

	repo := documents.NewRepository(pool)
	queue := jobs.NewQueue(pool)
	svc := documents.NewService(repo, nil, nil)
	indexer := documents.NewIndexer(repo, nil)

	doc, err := svc.CreateNote(ctx, user.ID, "Training notes", embedNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	fake := fakeEmbedder{dims: 1024}
	sweeper := documents.NewEmbedSweeper(repo, queue, fake.EmbedModel(), nil)
	if err = sweeper.HandleSweep(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatal("sweep should enqueue embed job for user with gap")
	}
	if claimed.Kind != jobs.KindEmbedChunks {
		t.Fatalf("kind = %q", claimed.Kind)
	}
}

// A passage the provider refuses fails its job three times over. Before the
// sweep looked at failures it queued the same job again every fifteen minutes,
// forever, and each pass paid for the provider calls again.
func TestSweepLeavesAUserAloneAfterARecentFailure(t *testing.T) {
	kit := newSweepAfterFailure(t, "sweep-failed-recent@north.test")

	if err := kit.sweeper.HandleSweep(kit.ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := kit.queue.Claim(kit.ctx); err != nil || ok {
		t.Fatalf("sweep queued work for a user whose embed job just failed: ok=%v err=%v", ok, err)
	}
}

// A provider outage fails jobs too, so a failure must not stop a person's
// library being embedded for good.
func TestSweepRetriesAUserOnceTheFailureHasAged(t *testing.T) {
	kit := newSweepAfterFailure(t, "sweep-failed-old@north.test")

	if _, err := kit.pool.Exec(kit.ctx, `UPDATE jobs SET updated_at = now() - interval '7 hours' WHERE status = 'failed'`); err != nil {
		t.Fatal(err)
	}

	if err := kit.sweeper.HandleSweep(kit.ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	claimed, ok, err := kit.queue.Claim(kit.ctx)
	if err != nil || !ok {
		t.Fatalf("sweep did not retry an old failure: ok=%v err=%v", ok, err)
	}
	if claimed.Kind != jobs.KindEmbedChunks {
		t.Fatalf("kind = %q", claimed.Kind)
	}
}

type sweepAfterFailure struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	queue   *jobs.Queue
	sweeper *documents.EmbedSweeper
}

// newSweepAfterFailure leaves a person with passages missing vectors and one
// embed job that has run out of attempts.
func newSweepAfterFailure(t *testing.T, email string) sweepAfterFailure {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, email)

	repo := documents.NewRepository(pool)
	queue := jobs.NewQueue(pool)

	doc, err := documents.NewService(repo, nil, nil).CreateNote(ctx, user.ID, "Training notes", embedNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = documents.NewIndexer(repo, nil).IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	if _, err = queue.Enqueue(ctx, jobs.KindEmbedChunks, jobs.EmbedChunksPayload{UserID: user.ID}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim embed job: ok=%v err=%v", ok, err)
	}
	if err = queue.Fail(ctx, claimed.ID, "provider refused a passage"); err != nil {
		t.Fatal(err)
	}

	return sweepAfterFailure{
		ctx:     ctx,
		pool:    pool,
		queue:   queue,
		sweeper: documents.NewEmbedSweeper(repo, queue, fakeEmbedder{dims: 1024}.EmbedModel(), nil),
	}
}

func TestSearchPageSupportsOffset(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "search-page@north.test")

	repo := documents.NewRepository(pool)
	svc := documents.NewService(repo, nil, nil)
	indexer := documents.NewIndexer(repo, nil)

	doc, err := svc.CreateNote(ctx, user.ID, "Training notes", embedNote)
	if err != nil {
		t.Fatal(err)
	}
	if err = indexer.IndexDocument(ctx, user.ID, doc.ID); err != nil {
		t.Fatal(err)
	}

	hits, hasMore, err := svc.SearchPage(ctx, user.ID, "overhead pressing grip", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("first page = %d hits, want 1", len(hits))
	}

	more, _, err := svc.SearchPage(ctx, user.ID, "overhead pressing grip", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) == 0 && hasMore {
		t.Fatal("expected a second page when hasMore is true")
	}
}
