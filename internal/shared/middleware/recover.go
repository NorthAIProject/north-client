package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover turns a panic in a handler into a logged 500 rather than a dead
// process. The stack trace goes to the log; the user gets a generic message,
// because a Go stack trace in a browser is both useless to them and a
// disclosure of internals.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			// A client that disconnects mid-write makes net/http panic with
			// ErrAbortHandler by design. That is not a bug and must not be
			// logged as one; it is routine for long-lived SSE streams.
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}

			FromContext(r.Context()).Error("panic recovered",
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())),
			)

			// Only meaningful if nothing has been written yet. For a stream
			// already in flight the connection simply ends, which the client
			// sees as a truncated response.
			http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		}()

		next.ServeHTTP(w, r)
	})
}
