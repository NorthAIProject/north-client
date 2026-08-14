package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/google/uuid"
)

type payload struct {
	Value string `json:"value"`
}

func newQueue(t *testing.T) *jobs.Queue {
	t.Helper()
	return jobs.NewQueue(testdb.New(t))
}

func TestEnqueueAndClaim(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, jobs.KindAnalyzeFormVideo, payload{Value: "hello"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, ok, err := q.Claim(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok {
		t.Fatal("expected to claim the queued job")
	}
	if job.Kind != jobs.KindAnalyzeFormVideo {
		t.Fatalf("kind = %q", job.Kind)
	}
	if job.Attempts != 1 {
		t.Fatalf("claiming should count as an attempt, got %d", job.Attempts)
	}

	var decoded payload
	if err := json.Unmarshal(job.Payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.Value != "hello" {
		t.Fatalf("payload = %+v", decoded)
	}
}

func TestClaimOnEmptyQueueIsNotAnError(t *testing.T) {
	q := newQueue(t)

	// The worker polls constantly; an empty queue is the common case and must
	// not be reported as a failure.
	_, ok, err := q.Claim(context.Background())
	if err != nil {
		t.Fatalf("claim on empty queue: %v", err)
	}
	if ok {
		t.Fatal("claimed a job from an empty queue")
	}
}

// The property that makes a table into a queue: two workers claiming at once
// must get different jobs, never the same one twice.
func TestConcurrentWorkersNeverClaimTheSameJob(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	const total = 20
	for i := 0; i < total; i++ {
		if _, err := q.Enqueue(ctx, jobs.KindAnalyzeFormVideo, payload{Value: "x"}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	var (
		mu      sync.Mutex
		claimed = map[string]int{}
		wg      sync.WaitGroup
	)

	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job, ok, err := q.Claim(ctx)
				if err != nil || !ok {
					return
				}
				mu.Lock()
				claimed[job.ID.String()]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(claimed) != total {
		t.Fatalf("expected all %d jobs claimed exactly once, got %d distinct", total, len(claimed))
	}
	for id, times := range claimed {
		if times != 1 {
			t.Errorf("job %s was claimed %d times", id, times)
		}
	}
}

func TestRetryMakesAJobEligibleAgainLater(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, jobs.KindAnalyzeFormVideo, payload{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, _, _ := q.Claim(ctx)

	if err := q.Retry(ctx, job.ID, time.Now().Add(time.Hour), "provider was down"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	// Scheduled for the future, so it must not come back immediately.
	if _, ok, _ := q.Claim(ctx); ok {
		t.Fatal("a job scheduled for later was claimed now")
	}

	// Backdating makes it eligible, which is what the passage of time would do.
	if err := q.Retry(ctx, job.ID, time.Now().Add(-time.Minute), "still down"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	again, ok, _ := q.Claim(ctx)
	if !ok {
		t.Fatal("an eligible retry was not claimed")
	}
	if again.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", again.Attempts)
	}
	if again.LastError != "still down" {
		t.Fatalf("last error = %q", again.LastError)
	}
}

func TestCompletedAndFailedJobsAreNotClaimedAgain(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := q.Enqueue(ctx, jobs.KindAnalyzeFormVideo, payload{}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	first, _, _ := q.Claim(ctx)
	second, _, _ := q.Claim(ctx)

	if err := q.Complete(ctx, first.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := q.Fail(ctx, second.ID, "gave up"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	if _, ok, _ := q.Claim(ctx); ok {
		t.Fatal("a finished job was claimed again")
	}
}

// A worker that dies mid-job leaves it 'running'. Without recovery the work
// silently never happens, which for a form analysis is a user watching a
// spinner that will never resolve.
func TestStaleRunningJobsAreReturnedToTheQueue(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, jobs.KindAnalyzeFormVideo, payload{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, ok, _ := q.Claim(ctx); !ok {
		t.Fatal("expected to claim the job")
	}

	// Nothing to claim while it is held.
	if _, ok, _ := q.Claim(ctx); ok {
		t.Fatal("a running job was claimed by a second worker")
	}

	// Zero duration treats everything currently running as abandoned.
	released, err := q.ReleaseStale(ctx, 0)
	if err != nil {
		t.Fatalf("release stale: %v", err)
	}
	if released != 1 {
		t.Fatalf("released %d jobs, want 1", released)
	}

	if _, ok, _ := q.Claim(ctx); !ok {
		t.Fatal("a released job should be claimable again")
	}
}

func TestBackoffGrows(t *testing.T) {
	t.Parallel()

	first, second, third := jobs.Backoff(1), jobs.Backoff(2), jobs.Backoff(3)

	if first >= second || second >= third {
		t.Fatalf("backoff should grow: %v, %v, %v", first, second, third)
	}
}

func TestRequeueFailedEmbedJobsForUser(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()
	userID := uuid.New()

	if _, err := q.Enqueue(ctx, jobs.KindEmbedChunks, jobs.EmbedChunksPayload{UserID: userID}); err != nil {
		t.Fatal(err)
	}
	job, ok, err := q.Claim(ctx)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err = q.Fail(ctx, job.ID, "provider down"); err != nil {
		t.Fatal(err)
	}

	n, err := q.RequeueFailedEmbedJobsForUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("requeued %d jobs, want 1", n)
	}

	again, ok, err := q.Claim(ctx)
	if err != nil || !ok {
		t.Fatal("requeued job should be claimable")
	}
	if again.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 after requeue", again.Attempts)
	}
}

func TestHasPendingJobForUser(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()
	userID := uuid.New()

	pending, err := q.HasPendingJobForUser(ctx, jobs.KindEmbedChunks, userID)
	if err != nil {
		t.Fatal(err)
	}
	if pending {
		t.Fatal("no jobs yet")
	}

	if _, err = q.Enqueue(ctx, jobs.KindEmbedChunks, jobs.EmbedChunksPayload{UserID: userID}); err != nil {
		t.Fatal(err)
	}

	pending, err = q.HasPendingJobForUser(ctx, jobs.KindEmbedChunks, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("embed job should be pending")
	}

	var claimed bool
	if _, claimed, err = q.Claim(ctx); err != nil || !claimed {
		t.Fatal(err)
	}

	pending, err = q.HasPendingJobForUser(ctx, jobs.KindEmbedChunks, userID)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("running embed job should count as pending")
	}
}

// A malformed payload must fail one job, not take down the worker and every
// other job with it.
func TestAPanickingHandlerFailsOnlyItsOwnJob(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, jobs.KindAnalyzeFormVideo, payload{Value: "boom"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	worker := jobs.NewWorker(q, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.Register(jobs.KindAnalyzeFormVideo, func(context.Context, json.RawMessage) error {
		panic("handler exploded")
	})

	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx) }()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("worker returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not survive a panicking handler")
	}
}
