// Package profiling serves Go's runtime profiles — CPU, heap, goroutines,
// allocations, execution traces — on a listener of their own.
//
// Never on the public router. A CPU profile costs the process the seconds it
// is asked for, and a heap or goroutine dump describes the internals to
// whoever requests one. The listener is loopback by default, so the way in is
// a port-forward to the pod:
//
//	kubectl -n horus port-forward deploy/khepri-web 6060
//	go tool pprof -http=:8081 'http://localhost:6060/debug/pprof/profile?seconds=30'
//
// pprof's web UI draws the flame graph (View → Flame Graph).
package profiling

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"
)

// Handler routes the standard /debug/pprof endpoints on a mux of its own.
//
// net/http/pprof also registers itself on http.DefaultServeMux when imported.
// Nothing in this codebase serves that mux, which is why this builds its own
// rather than handing it out.
func Handler() http.Handler {
	mux := http.NewServeMux()
	// Index also serves every named profile below it: heap, goroutine,
	// allocs, block, mutex, threadcreate.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Start serves Handler on addr in the background and returns a func that
// closes the listener. The process carries on if the port is taken: losing the
// profiler is no reason to stop serving.
//
// No write timeout, on purpose: a CPU profile or a trace holds the response
// open for exactly as long as it was asked to record.
func Start(addr string, log *slog.Logger) (stop func()) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info("profiling listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("profiling listener stopped", slog.Any("error", err))
		}
	}()
	return func() { _ = srv.Close() }
}
