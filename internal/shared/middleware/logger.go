package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

var loggerKey = ctxValue[*slog.Logger]{key: "logger"}

// Logger records one structured line per request and puts a request-scoped
// logger into the context.
//
// Handlers and services should take their logger from the context rather than
// reaching for a package-level default, so that every line they emit is
// automatically correlated with the request that caused it.
func Logger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			log := base.With(
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)

			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(loggerKey.set(r.Context(), log)))

			// Server errors are the ones worth waking up for; everything else
			// is routine traffic and stays at info.
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}

			log.LogAttrs(r.Context(), level, "request",
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.written),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// FromContext returns the request-scoped logger, falling back to the default
// logger outside an HTTP request so callers never have to nil-check.
func FromContext(ctx context.Context) *slog.Logger {
	if log, ok := loggerKey.get(ctx); ok {
		return log
	}
	return slog.Default()
}

// responseRecorder captures the status and size that were actually written.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer. Without it,
// SSE handlers could not flush through this wrapper, which would silently turn
// streamed AI responses into one buffered blob at the end.
func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
