package jobs_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// settableClock is a worker clock a test can move while the loop reads it.
type settableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *settableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *settableClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func probe(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// The threshold has to outlast the longest job a worker may legitimately be
// running, or the kubelet kills exactly the worker doing its job properly.
func TestTheLivenessThresholdOutlastsTheLongestJob(t *testing.T) {
	if want := 15*time.Minute + time.Minute + 2*time.Minute; jobs.LivenessThreshold != want {
		t.Errorf("threshold = %s, want jobTimeout + shutdownGrace + 2m = %s", jobs.LivenessThreshold, want)
	}
}

// A worker that has not reached its first claim yet is starting, not stuck: a
// failing probe during startup would restart it before it ever got there.
func TestAWorkerAtStartupIsAlive(t *testing.T) {
	clock := &settableClock{now: time.Unix(1_790_000_000, 0)}
	worker := jobs.NewWorker(nil, quietLog()).WithClock(clock.Now)
	h := worker.ProbeHandler(http.NotFoundHandler())

	if rec := probe(t, h, "/livez"); rec.Code != http.StatusOK {
		t.Errorf("fresh worker: status = %d, want 200", rec.Code)
	}

	clock.Set(clock.Now().Add(jobs.LivenessThreshold))
	if rec := probe(t, h, "/livez"); rec.Code != http.StatusOK {
		t.Errorf("exactly at the threshold: status = %d, want 200", rec.Code)
	}
}

// Startup does not buy forever: a worker that has never once reached the queue
// is the failure the probe is for, and restarting is all a probe can do about it.
func TestAWorkerThatNeverClaimsEventuallyFailsTheProbe(t *testing.T) {
	clock := &settableClock{now: time.Unix(1_790_000_000, 0)}
	worker := jobs.NewWorker(nil, quietLog()).WithClock(clock.Now)
	h := worker.ProbeHandler(http.NotFoundHandler())

	clock.Set(clock.Now().Add(jobs.LivenessThreshold + time.Second))
	if rec := probe(t, h, "/livez"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 past the threshold with no tick", rec.Code)
	}
}

// The metrics endpoint keeps answering when the loop is stuck, because a scrape
// of a stuck worker is what an operator wants most.
func TestOtherPathsKeepServingWhenTheLoopIsStuck(t *testing.T) {
	clock := &settableClock{now: time.Unix(1_790_000_000, 0)}
	worker := jobs.NewWorker(nil, quietLog()).WithClock(clock.Now)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := worker.ProbeHandler(next)

	clock.Set(clock.Now().Add(jobs.LivenessThreshold + time.Hour))
	if rec := probe(t, h, "/livez"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/livez: status = %d, want 503", rec.Code)
	}
	for _, path := range []string{"/", "/metrics", "/healthz", "/anything"} {
		if rec := probe(t, h, path); rec.Code != http.StatusTeapot {
			t.Errorf("%s: status = %d, want it to reach the metrics handler", path, rec.Code)
		}
	}
}

// A loop that reaches the queue stays alive however long it runs; one that
// stops reaching it is noticed. This one runs the real loop against a database.
func TestALoopThatKeepsClaimingStaysAliveAndAStuckOneDoesNot(t *testing.T) {
	q := jobs.NewQueue(testdb.New(t))
	start := time.Unix(1_790_000_000, 0)
	clock := &settableClock{now: start}

	entered := make(chan struct{})
	release := make(chan struct{})

	worker := jobs.NewWorker(q, quietLog()).WithClock(clock.Now)
	worker.Register(jobs.Kind("liveness_stuck"), func(context.Context, json.RawMessage) error {
		close(entered)
		<-release // ignores its context, like the handler the probe exists for
		return nil
	})
	h := worker.ProbeHandler(http.NotFoundHandler())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	// An idle loop polls every two seconds, so its heartbeat keeps moving. Step
	// the clock well past the threshold and wait for a poll to catch it up.
	clock.Set(start.Add(jobs.LivenessThreshold + time.Hour))
	deadline := time.Now().Add(10 * time.Second)
	for probe(t, h, "/livez").Code != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatal("an idle loop that is polling the queue never reported alive")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := q.Enqueue(context.Background(), jobs.Kind("liveness_stuck"), payload{Value: "x"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the job never started")
	}

	// A job running to the very end of its time is legitimate.
	clock.Set(clock.Now().Add(15 * time.Minute))
	if rec := probe(t, h, "/livez"); rec.Code != http.StatusOK {
		t.Errorf("mid-job at jobTimeout: status = %d, want 200", rec.Code)
	}

	// Past the threshold, the handler is stuck.
	clock.Set(clock.Now().Add(jobs.LivenessThreshold))
	if rec := probe(t, h, "/livez"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("stuck handler: status = %d, want 503", rec.Code)
	}

	close(release)
}
