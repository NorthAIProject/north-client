package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

var requestIDKey = ctxValue[string]{key: "request_id"}

// RequestID attaches an identifier to every request and echoes it back in the
// X-Request-ID header. Every log line for the request carries the same value,
// which is what makes a production log searchable when a user reports a
// problem and can only tell you roughly when it happened.
//
// An inbound X-Request-ID is honoured so a reverse proxy or a caller that
// already has a trace identifier keeps one identity end to end.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 200 {
			id = uuid.NewString()
		}

		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(requestIDKey.set(r.Context(), id)))
	})
}

// RequestIDFrom returns the identifier attached by RequestID, or "" outside an
// HTTP request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := requestIDKey.get(ctx)
	return id
}

// WithRequestID attaches an identifier to a context that no HTTP request
// created.
//
// The worker is why this exists. A job carries the id of the request that
// queued it, and the handler running that job is not in a request — so the id
// has to be put back on the context by hand for the job's own log lines, and
// for anything it enqueues in turn, to stay part of the same story.
//
// An empty id leaves the context alone rather than storing one: a job the
// worker queued itself has no request behind it, and an empty string in the
// logs would look like a value that failed to propagate.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return requestIDKey.set(ctx, id)
}
