package jobs_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestEnqueuePeriodicKeepsOneCopyPerKind(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	first, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, struct{}{})
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if !first {
		t.Error("the first enqueue reported a skip")
	}

	second, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, struct{}{})
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if second {
		t.Error("a second copy was added while the first was pending")
	}

	other, err := q.EnqueuePeriodic(ctx, jobs.KindSweepOAuth, struct{}{})
	if err != nil {
		t.Fatalf("other kind: %v", err)
	}
	if !other {
		t.Error("one kind's pending copy blocked a different kind")
	}
}

func TestConcurrentPeriodicEnqueuesAddExactlyOne(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	const callers = 16
	results := make(chan bool, callers)
	errs := make(chan error, callers)

	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			added, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, struct{}{})
			if err != nil {
				errs <- err
				return
			}
			results <- added
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Errorf("enqueue: %v", err)
	}
	added := 0
	for r := range results {
		if r {
			added++
		}
	}
	if added != 1 {
		t.Errorf("%d of %d concurrent enqueues added a job, want exactly 1", added, callers)
	}
}

// Only a periodic sweep is one of a kind. Two messages or two people queueing the
// same kind of job are separate work, and must both stay queued.
func TestEnqueueWithAPayloadIsNeverDeduplicated(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	for range 2 {
		if _, err := q.Enqueue(ctx, jobs.KindAnalyzeFormVideo, payload{Value: "same"}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		added, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, payload{Value: "same"})
		if err != nil {
			t.Fatalf("periodic enqueue with a payload: %v", err)
		}
		if !added {
			t.Error("a periodic job with a payload was skipped")
		}
	}
}

// Every way a running job returns to the queue has to survive a tick having
// arrived during the run. An index on pending rows alone fails all three of these
// with a unique violation.
func TestARunningPeriodicJobCanReturnToTheQueue(t *testing.T) {
	cases := map[string]func(t *testing.T, q *jobs.Queue, pool *pgxpool.Pool, job jobs.Job){
		"retry": func(t *testing.T, q *jobs.Queue, _ *pgxpool.Pool, job jobs.Job) {
			t.Helper()
			if err := q.Retry(context.Background(), job.ID, time.Now(), "failed"); err != nil {
				t.Fatalf("retry: %v", err)
			}
		},
		"requeue after shutdown": func(t *testing.T, q *jobs.Queue, _ *pgxpool.Pool, job jobs.Job) {
			t.Helper()
			if err := q.Requeue(context.Background(), job.ID, "interrupted"); err != nil {
				t.Fatalf("requeue: %v", err)
			}
		},
		"stale release": func(t *testing.T, q *jobs.Queue, pool *pgxpool.Pool, job jobs.Job) {
			t.Helper()
			ctx := context.Background()
			if _, err := pool.Exec(ctx, `UPDATE jobs SET updated_at = now() - interval '1 hour' WHERE id = $1`, job.ID); err != nil {
				t.Fatalf("age job: %v", err)
			}
			if _, err := q.ReleaseStale(ctx, time.Minute); err != nil {
				t.Fatalf("release stale: %v", err)
			}
		},
	}

	for name, returnToQueue := range cases {
		t.Run(name, func(t *testing.T) {
			pool := testdb.New(t)
			q := jobs.NewQueue(pool)
			ctx := context.Background()

			if _, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, struct{}{}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			job, ok, err := q.Claim(ctx)
			if err != nil || !ok {
				t.Fatalf("claim: ok=%v err=%v", ok, err)
			}

			// A tick lands while the job runs.
			added, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, struct{}{})
			if err != nil {
				t.Fatalf("tick during the run: %v", err)
			}
			if added {
				t.Error("a tick added a copy while the job was running")
			}

			returnToQueue(t, q, pool, job)

			if status := jobStatus(t, pool, job.ID); status != "pending" {
				t.Errorf("status = %q, want pending", status)
			}
		})
	}
}

func TestAFinishedPeriodicJobFreesItsSlot(t *testing.T) {
	cases := map[string]func(q *jobs.Queue, job jobs.Job) error{
		"done": func(q *jobs.Queue, job jobs.Job) error {
			return q.Complete(context.Background(), job.ID)
		},
		"failed": func(q *jobs.Queue, job jobs.Job) error {
			return q.Fail(context.Background(), job.ID, "out of attempts")
		},
	}

	for name, finish := range cases {
		t.Run(name, func(t *testing.T) {
			q := newQueue(t)
			ctx := context.Background()

			if _, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, struct{}{}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			job, ok, err := q.Claim(ctx)
			if err != nil || !ok {
				t.Fatalf("claim: ok=%v err=%v", ok, err)
			}
			if err = finish(q, job); err != nil {
				t.Fatalf("finish: %v", err)
			}

			added, err := q.EnqueuePeriodic(ctx, jobs.KindSweepWatches, struct{}{})
			if err != nil {
				t.Fatalf("next tick: %v", err)
			}
			if !added {
				t.Error("the next tick was skipped after the job finished")
			}
		})
	}
}

// The worker has one lane, so a slow handler holds up every sweep behind it.
// Ticks that keep arriving during the stall must not leave a backlog of the same
// sweep to run one after another, and ticking must resume once the stall ends.
func TestAStalledLaneDoesNotPileUpPeriodicJobs(t *testing.T) {
	pool := testdb.New(t)
	q := jobs.NewQueue(pool)
	ctx := context.Background()

	started := make(chan struct{})
	release := make(chan struct{})
	var first sync.Once

	worker := jobs.NewWorker(q, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.Register(jobs.KindSweepWatches, func(context.Context, json.RawMessage) error {
		first.Do(func() {
			close(started)
			<-release
		})
		return nil
	})
	worker.RegisterPeriodic(20*time.Millisecond, jobs.KindSweepWatches, struct{}{})

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx) }()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		close(release)
		cancel()
		t.Fatal("the sweep never ran")
	}

	// Long enough for many ticks to arrive while the handler is held.
	time.Sleep(300 * time.Millisecond)

	var active int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE kind = $1 AND status IN ('pending', 'running')`,
		string(jobs.KindSweepWatches),
	).Scan(&active)
	if err != nil {
		t.Errorf("count active jobs: %v", err)
	}
	if active != 1 {
		t.Errorf("active jobs during the stall = %d, want 1", active)
	}

	close(release)

	deadline := time.Now().Add(15 * time.Second)
	finished := 0
	for finished < 2 && time.Now().Before(deadline) {
		if err = pool.QueryRow(ctx,
			`SELECT count(*) FROM jobs WHERE kind = $1 AND status = 'done'`,
			string(jobs.KindSweepWatches),
		).Scan(&finished); err != nil {
			t.Errorf("count finished jobs: %v", err)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if finished < 2 {
		t.Errorf("finished runs after the stall = %d, want at least 2: ticking did not resume", finished)
	}

	cancel()
	if err = <-done; err != nil {
		t.Fatalf("worker stopped with error: %v", err)
	}
}
