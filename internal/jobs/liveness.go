package jobs

import (
	"fmt"
	"net/http"
	"time"
)

// LivenessThreshold is how long the loop may go without reaching the queue
// before the worker counts as stuck.
//
// The heartbeat only moves on a Claim that reached the database, and the loop
// makes no claim while a job runs, so a healthy worker can be silent for as long
// as its longest job. jobTimeout bounds that and shutdownGrace is the most a
// stop can add; the two minutes on top cover bookkeepingTimeout and the next
// claim. Anything tighter would have the kubelet kill a worker for working.
const LivenessThreshold = jobTimeout + shutdownGrace + 2*time.Minute

// livePath is the one path the probe handler keeps for itself.
const livePath = "/livez"

// tick records that the loop just reached the queue.
func (w *Worker) tick() {
	now := w.now()
	w.lastTick.Store(&now)
}

// sinceTick is how long ago the loop last reached the queue. Before the first
// claim it counts from when the worker was built, so startup reads as alive and
// a worker that never reaches the queue still runs out of time.
func (w *Worker) sinceTick() time.Duration {
	return w.now().Sub(*w.lastTick.Load())
}

// ProbeHandler answers /livez from the loop's heartbeat and hands every other
// path to next, which is the metrics handler on the worker's listener.
//
// The heartbeat is the point. The worker's process being up says nothing: it
// once ran no jobs for eleven days while every probe passed. Failing here
// catches a handler that ignores its context and a loop spinning on a dead pool,
// and a restart fixes the first and costs nothing for the second.
func (w *Worker) ProbeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != livePath {
			next.ServeHTTP(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.Header().Set("Cache-Control", "no-store")
		if age := w.sinceTick(); age > LivenessThreshold {
			rw.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(rw, "worker loop last reached the queue %s ago\n", age.Round(time.Second))
			return
		}
		_, _ = fmt.Fprintln(rw, "ok")
	})
}
