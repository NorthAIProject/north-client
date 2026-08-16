package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// pollInterval is how long the worker waits when the queue is empty.
//
// Polling rather than LISTEN/NOTIFY: at North's scale a two-second delay on a
// job that takes a minute to run is irrelevant, and polling has no notification
// to miss during a reconnect.
const pollInterval = 2 * time.Second

// staleAfter is how long a 'running' job may go without an update before it is
// assumed abandoned by a dead worker.
const staleAfter = 30 * time.Minute

// jobTimeout bounds a single handler run.
const jobTimeout = 15 * time.Minute

// Handler performs one kind of job. Payload is the JSON it was enqueued with.
type Handler func(ctx context.Context, payload json.RawMessage) error

// Worker claims jobs and runs their handlers.
type Worker struct {
	queue     *Queue
	log       *slog.Logger
	handlers  map[Kind]Handler
	periodics []periodicEnqueue
}

type periodicEnqueue struct {
	interval time.Duration
	kind     Kind
	payload  any
}

func NewWorker(queue *Queue, log *slog.Logger) *Worker {
	return &Worker{queue: queue, log: log, handlers: make(map[Kind]Handler)}
}

// RegisterPeriodic enqueues a job on a fixed interval until ctx is cancelled.
func (w *Worker) RegisterPeriodic(interval time.Duration, kind Kind, payload any) {
	w.periodics = append(w.periodics, periodicEnqueue{interval: interval, kind: kind, payload: payload})
}

// Register attaches a handler to a job kind.
func (w *Worker) Register(kind Kind, handler Handler) {
	w.handlers[kind] = handler
}

// Run processes jobs until ctx is cancelled.
//
// Single-threaded per worker on purpose. Concurrency comes from running more
// worker processes, which SKIP LOCKED already supports, rather than from a
// goroutine pool that would make one crash lose several jobs at once.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("worker started", slog.Any("kinds", w.kinds()))

	for _, p := range w.periodics {
		go w.runPeriodic(ctx, p)
	}

	// Anything left 'running' by a previous process that died is returned to
	// the queue before this one starts taking new work.
	if n, err := w.queue.ReleaseStale(ctx, staleAfter); err != nil {
		w.log.Error("could not release stale jobs", slog.Any("error", err))
	} else if n > 0 {
		w.log.Warn("released jobs abandoned by a previous worker", slog.Int64("count", n))
	}

	staleTicker := time.NewTicker(staleAfter / 2)
	defer staleTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker stopped")
			return nil
		case <-staleTicker.C:
			if _, err := w.queue.ReleaseStale(ctx, staleAfter); err != nil {
				w.log.Error("could not release stale jobs", slog.Any("error", err))
			}
		default:
		}

		claimed, err := w.claimAndRun(ctx)
		if err != nil {
			w.log.Error("worker loop error", slog.Any("error", err))
		}

		if claimed {
			continue // drain the queue before sleeping
		}

		select {
		case <-ctx.Done():
			w.log.Info("worker stopped")
			return nil
		case <-time.After(pollInterval):
		}
	}
}

// claimAndRun takes one job and runs it, reporting whether there was one.
func (w *Worker) claimAndRun(ctx context.Context) (bool, error) {
	job, ok, err := w.queue.Claim(ctx)
	if err != nil || !ok {
		return false, err
	}

	log := w.log.With(
		slog.String("job_id", job.ID.String()),
		slog.String("kind", string(job.Kind)),
		slog.Int("attempt", job.Attempts),
	)
	if job.RequestID != "" {
		// Only when there is one. A periodic sweep has no request behind it,
		// and an empty field would read as an id that failed to propagate
		// rather than one that never existed.
		log = log.With(slog.String("request_id", job.RequestID))

		// Back onto the context too, so anything this handler enqueues in turn
		// stays attached to the same request rather than starting a new story.
		ctx = middleware.WithRequestID(ctx, job.RequestID)
	}

	handler, known := w.handlers[job.Kind]
	if !known {
		// Nothing here can run it, and retrying will not change that.
		log.Error("no handler registered for this job kind")
		return true, w.queue.Fail(ctx, job.ID, "no handler registered for kind "+string(job.Kind))
	}

	// Detached from the loop's context so shutdown lets the current job finish
	// rather than abandoning it half-done.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobTimeout)
	defer cancel()

	started := time.Now()
	err = runSafely(runCtx, handler, job.Payload)
	elapsed := time.Since(started)

	if err == nil {
		log.Info("job done", slog.Duration("took", elapsed))
		return true, w.queue.Complete(runCtx, job.ID)
	}

	if job.Exhausted() {
		log.Error("job failed permanently", slog.Any("error", err), slog.Int("attempts", job.Attempts))
		return true, w.queue.Fail(runCtx, job.ID, err.Error())
	}

	retryAt := time.Now().Add(Backoff(job.Attempts))
	log.Warn("job failed, will retry", slog.Any("error", err), slog.Time("retry_at", retryAt))

	return true, w.queue.Retry(runCtx, job.ID, retryAt, err.Error())
}

// runSafely turns a panicking handler into a failed job rather than a dead
// worker. One malformed payload must not stop every other job from running.
func runSafely(ctx context.Context, handler Handler, payload json.RawMessage) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("handler panicked: %v", rec)
		}
	}()

	return handler(ctx, payload)
}

func (w *Worker) kinds() []string {
	out := make([]string, 0, len(w.handlers))
	for kind := range w.handlers {
		out = append(out, string(kind))
	}
	return out
}

func (w *Worker) runPeriodic(ctx context.Context, p periodicEnqueue) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.queue.Enqueue(ctx, p.kind, p.payload); err != nil {
				w.log.Error("periodic enqueue failed", slog.String("kind", string(p.kind)), slog.Any("error", err))
			}
		}
	}
}
