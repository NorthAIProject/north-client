package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/metrics"
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

// bookkeepingTimeout bounds recording a run's outcome.
//
// Its own budget rather than the handler's: a handler that ran into
// jobTimeout returns on an expired context, and recording the retry on that
// same context would fail before reaching the database. The job would then sit
// in 'running' until the stale sweep and be run again from the top.
const bookkeepingTimeout = 10 * time.Second

// shutdownGrace is how long the current job may keep running once the worker is
// told to stop. After that its context is cancelled and it is requeued, rather
// than being killed outright and waiting out staleAfter. Below the pod's
// terminationGracePeriodSeconds in the infra repo.
const shutdownGrace = 60 * time.Second

// Handler performs one kind of job. Payload is the JSON it was enqueued with.
type Handler func(ctx context.Context, payload json.RawMessage) error

// Worker claims jobs and runs their handlers.
type Worker struct {
	queue     *Queue
	log       *slog.Logger
	metrics   *metrics.Registry
	handlers  map[Kind]Handler
	periodics []periodicEnqueue

	// now is the clock periodic alignment reads. Swapped in tests so they do
	// not have to wait for a real wall-clock boundary.
	now func() time.Time

	// jobTimeout bounds one handler run. A field so tests can hit it without
	// waiting fifteen minutes.
	jobTimeout time.Duration

	// shutdownGrace is the constant of that name. A field so tests do not wait
	// out a minute.
	shutdownGrace time.Duration
}

// WithClock fixes the worker's idea of now. Tests only.
func (w *Worker) WithClock(now func() time.Time) *Worker {
	w.now = now
	return w
}

// WithShutdownGrace changes how long a job may run on after shutdown. Tests only.
func (w *Worker) WithShutdownGrace(d time.Duration) *Worker {
	w.shutdownGrace = d
	return w
}

// WithJobTimeout changes how long one handler may run. Tests only.
func (w *Worker) WithJobTimeout(d time.Duration) *Worker {
	w.jobTimeout = d
	return w
}

type periodicEnqueue struct {
	interval time.Duration
	kind     Kind
	payload  any
}

func NewWorker(queue *Queue, log *slog.Logger) *Worker {
	return &Worker{queue: queue, log: log, handlers: make(map[Kind]Handler), now: time.Now, jobTimeout: jobTimeout, shutdownGrace: shutdownGrace}
}

// WithMetrics attaches counters. Returns the worker so it can be wired in one
// expression at startup; nil leaves counting off.
func (w *Worker) WithMetrics(m *metrics.Registry) *Worker {
	w.metrics = m
	return w
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
	if err == nil {
		// Only a claim that reached the database counts as a tick. A loop
		// spinning on a dead connection is the failure the heartbeat is for.
		w.metrics.WorkerHeartbeat(w.now())
	}
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
	// rather than abandoning it half-done, for as long as shutdownGrace allows.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.jobTimeout)
	defer cancel()

	grace := w.shutdownGrace
	stopWatching := context.AfterFunc(ctx, func() {
		timer := time.AfterFunc(grace, cancel)
		context.AfterFunc(runCtx, func() { timer.Stop() })
	})
	defer stopWatching()

	// The handler logs through the context like every other layer does, so it
	// gets the logger built above rather than the default one. Without this a
	// handler's own errors arrive with no job id, no kind, and no request —
	// which is the state an operator is least able to act on.
	runCtx = middleware.WithLogger(runCtx, log)

	started := time.Now()
	err = runSafely(runCtx, handler, job.Payload)
	elapsed := time.Since(started)

	// Counted once per run, not once per job: a job that fails twice and then
	// succeeds is three runs, and the retry rate is the interesting half of
	// that story.
	w.metrics.JobFinished(string(job.Kind), elapsed, err != nil)

	// Detached from runCtx, which may be the very deadline the handler just
	// ran into. See bookkeepingTimeout.
	bookCtx, cancelBook := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancelBook()

	if err == nil {
		log.Info("job done", slog.Duration("took", elapsed))
		return true, w.queue.Complete(bookCtx, job.ID)
	}

	// Ahead of the attempt check: running out of shutdown grace says nothing
	// about the job, so it must not be what fails it. Canceled rather than
	// DeadlineExceeded, because only the grace timer cancels runCtx while the
	// handler is still running.
	if ctx.Err() != nil && errors.Is(runCtx.Err(), context.Canceled) {
		log.Warn("job interrupted by shutdown, requeued", slog.Any("error", err))
		return true, w.queue.Requeue(bookCtx, job.ID, "interrupted by shutdown: "+err.Error())
	}

	if job.Exhausted() {
		log.Error("job failed permanently", slog.Any("error", err), slog.Int("attempts", job.Attempts))
		return true, w.queue.Fail(bookCtx, job.ID, err.Error())
	}

	retryAt := time.Now().Add(Backoff(job.Attempts))
	log.Warn("job failed, will retry", slog.Any("error", err), slog.Time("retry_at", retryAt))

	return true, w.queue.Retry(bookCtx, job.ID, retryAt, err.Error())
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
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(untilNext(w.now(), p.interval)):
			if _, err := w.queue.Enqueue(ctx, p.kind, p.payload); err != nil {
				w.log.Error("periodic enqueue failed", slog.String("kind", string(p.kind)), slog.Any("error", err))
			}
		}
	}
}

// untilNext is how long to wait for the next wall-clock boundary of interval.
//
// Periodic work used to run on a ticker started at process boot, which had two
// consequences. The mild one is drift: "hourly" meant an hour after this worker
// happened to start, so the same sweep ran at :17 on one deploy and :49 on the
// next, and comparing two days of logs meant first working out when each
// process began.
//
// The severe one is starvation. A ticker's first tick comes a full interval
// after it is created, so a worker that restarts more often than its longest
// interval never fires that job at all. A deploy loop, a crash loop, or simply
// shipping twice an hour was enough to stop the hourly sweeps running — and
// nothing would have reported it, because a job that is never enqueued produces
// no failure, no log line and no metric.
//
// Aligning to the clock fixes both: whatever time the process starts, the next
// boundary is at most one interval away and is the same instant for every
// worker.
func untilNext(now time.Time, interval time.Duration) time.Duration {
	if interval <= 0 {
		// A caller asking for a non-positive interval is a programming error,
		// but spinning on it would be worse than waiting.
		return time.Hour
	}

	// Truncate works on absolute time since the epoch, so the boundaries are
	// the same everywhere rather than following the server's timezone. That is
	// what makes them comparable across deployments; anything that has to
	// happen at a particular *local* hour decides that inside its own handler,
	// where the user's timezone is known.
	next := now.Truncate(interval).Add(interval)
	return next.Sub(now)
}
